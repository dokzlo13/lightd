package sse

import (
	"context"
	"sync"

	"github.com/rs/zerolog/log"

	"github.com/dokzlo13/lightd/internal/actions"
	"github.com/dokzlo13/lightd/internal/events"
	"github.com/dokzlo13/lightd/internal/events/middleware"
	"github.com/dokzlo13/lightd/internal/lua/exec"
)

// HandlerRegistry provides handler lookup functions
type HandlerRegistry interface {
	FindButtonHandler(resourceID, buttonAction string) *ButtonHandler
	FindConnectivityHandler(deviceID, status string) *ConnectivityHandler
	FindRotaryHandler(resourceID string) *RotaryHandler
	FindLightChangeHandlers(resourceID, resourceType string) []*LightChangeHandler
}

// RegisterHandlers subscribes to SSE events on the event bus and dispatches to handlers
func RegisterHandlers(
	ctx context.Context,
	registry HandlerRegistry,
	bus *events.Bus,
	invoker *actions.Invoker,
	luaExec exec.Executor,
) {
	registerButtonHandler(ctx, registry, bus, invoker, luaExec)
	registerConnectivityHandler(ctx, registry, bus, invoker, luaExec)
	registerRotaryHandler(ctx, registry, bus, invoker, luaExec)
	registerLightChangeHandler(ctx, registry, bus, invoker, luaExec)
}

// registerButtonHandler sets up button event handling via the event bus.
func registerButtonHandler(
	ctx context.Context,
	registry HandlerRegistry,
	bus *events.Bus,
	invoker *actions.Invoker,
	luaExec exec.Executor,
) {
	collectors := make(map[string]middleware.Collector)
	var mu sync.Mutex

	bus.Subscribe(events.EventTypeButton, func(event events.Event) {
		resourceID, _ := event.Data["resource_id"].(string)
		buttonAction, _ := event.Data["action"].(string)
		eventID, _ := event.Data["event_id"].(string)

		handler := registry.FindButtonHandler(resourceID, buttonAction)
		if handler == nil {
			return
		}

		log.Debug().
			Str("resource_id", resourceID).
			Str("action", buttonAction).
			Str("handler_action", handler.ActionName).
			Msg("Button event matched handler")

		// Build collector key
		collectorKey := resourceID + ":" + buttonAction

		mu.Lock()
		collector, ok := collectors[collectorKey]
		if !ok {
			collector = createButtonCollector(ctx, handler, invoker, luaExec)
			collectors[collectorKey] = collector
		}
		mu.Unlock()

		collector.AddEvent(map[string]any{
			"resource_id": resourceID,
			"action":      buttonAction,
			"event_id":    eventID,
		})
	})
}

// createButtonCollector creates a collector for button events
func createButtonCollector(
	ctx context.Context,
	handler *ButtonHandler,
	invoker *actions.Invoker,
	luaExec exec.Executor,
) middleware.Collector {
	onFlush := func(events []map[string]any) {
		luaExec.Do(ctx, func(workCtx context.Context) {
			var args map[string]any

			if handler.CollectorFactory != nil && handler.CollectorFactory.Reducer != nil {
				// Safe to call LState() here - we're inside Do() callback on Lua worker
				args = exec.CallReducer(luaExec.LState(), handler.CollectorFactory.Reducer, events)
			} else if len(events) > 0 {
				args = events[0]
			} else {
				args = make(map[string]any)
			}

			// Merge with static action args
			for k, v := range handler.ActionArgs {
				args[k] = v
			}

			// Get event_id from args for idempotency
			eid, _ := args["event_id"].(string)
			delete(args, "event_id")
			delete(args, "resource_id")
			delete(args, "action")

			// Invoke action with button event ID as idempotency key
			invoker.Invoke(workCtx, handler.ActionName, args, eid)
		})
	}

	if handler.CollectorFactory != nil {
		return handler.CollectorFactory.Create(onFlush)
	}
	return middleware.NewImmediateCollector(onFlush)
}

// registerConnectivityHandler sets up connectivity event handling via the event bus.
func registerConnectivityHandler(
	ctx context.Context,
	registry HandlerRegistry,
	bus *events.Bus,
	invoker *actions.Invoker,
	luaExec exec.Executor,
) {
	collectors := make(map[string]middleware.Collector)
	var mu sync.Mutex

	bus.Subscribe(events.EventTypeConnectivity, func(event events.Event) {
		deviceID, _ := event.Data["device_id"].(string)
		status, _ := event.Data["status"].(string)

		handler := registry.FindConnectivityHandler(deviceID, status)
		if handler == nil {
			return
		}

		log.Debug().
			Str("device_id", deviceID).
			Str("status", status).
			Str("handler_action", handler.ActionName).
			Msg("Connectivity event matched handler")

		// Build collector key
		collectorKey := deviceID + ":" + status

		mu.Lock()
		collector, ok := collectors[collectorKey]
		if !ok {
			collector = createConnectivityCollector(ctx, handler, invoker, luaExec)
			collectors[collectorKey] = collector
		}
		mu.Unlock()

		collector.AddEvent(map[string]any{
			"device_id": deviceID,
			"status":    status,
		})
	})
}

// createConnectivityCollector creates a collector for connectivity events
func createConnectivityCollector(
	ctx context.Context,
	handler *ConnectivityHandler,
	invoker *actions.Invoker,
	luaExec exec.Executor,
) middleware.Collector {
	onFlush := func(events []map[string]any) {
		luaExec.Do(ctx, func(workCtx context.Context) {
			var args map[string]any

			if handler.CollectorFactory != nil && handler.CollectorFactory.Reducer != nil {
				// Safe to call LState() here - we're inside Do() callback on Lua worker
				args = exec.CallReducer(luaExec.LState(), handler.CollectorFactory.Reducer, events)
			} else if len(events) > 0 {
				args = events[0]
			} else {
				args = make(map[string]any)
			}

			// Merge with static action args
			for k, v := range handler.ActionArgs {
				args[k] = v
			}

			// Remove event metadata from args
			delete(args, "device_id")
			delete(args, "status")

			if err := invoker.Invoke(workCtx, handler.ActionName, args, ""); err != nil {
				log.Error().Err(err).Str("action", handler.ActionName).Msg("Failed to invoke connectivity action")
			}
		})
	}

	if handler.CollectorFactory != nil {
		return handler.CollectorFactory.Create(onFlush)
	}
	return middleware.NewImmediateCollector(onFlush)
}

// registerRotaryHandler sets up rotary event handling via the event bus.
func registerRotaryHandler(
	ctx context.Context,
	registry HandlerRegistry,
	bus *events.Bus,
	invoker *actions.Invoker,
	luaExec exec.Executor,
) {
	collectors := make(map[string]middleware.Collector)
	var mu sync.Mutex

	bus.Subscribe(events.EventTypeRotary, func(event events.Event) {
		resourceID, _ := event.Data["resource_id"].(string)
		direction, _ := event.Data["direction"].(string)
		steps, _ := event.Data["steps"].(int)

		handler := registry.FindRotaryHandler(resourceID)
		if handler == nil {
			return
		}

		mu.Lock()
		collector, ok := collectors[resourceID]
		if !ok {
			collector = createRotaryCollector(ctx, handler, invoker, luaExec)
			collectors[resourceID] = collector
		}
		mu.Unlock()

		collector.AddEvent(map[string]any{
			"direction": direction,
			"steps":     steps,
		})
	})
}

// createRotaryCollector creates a collector for rotary events
func createRotaryCollector(
	ctx context.Context,
	handler *RotaryHandler,
	invoker *actions.Invoker,
	luaExec exec.Executor,
) middleware.Collector {
	onFlush := func(events []map[string]any) {
		luaExec.Do(ctx, func(workCtx context.Context) {
			var args map[string]any

			if handler.CollectorFactory != nil && handler.CollectorFactory.Reducer != nil {
				// Safe to call LState() here - we're inside Do() callback on Lua worker
				args = exec.CallReducer(luaExec.LState(), handler.CollectorFactory.Reducer, events)
			} else if len(events) > 0 {
				args = events[0]
			} else {
				args = make(map[string]any)
			}

			// Merge with static action args
			for k, v := range handler.ActionArgs {
				args[k] = v
			}

			if err := invoker.Invoke(workCtx, handler.ActionName, args, ""); err != nil {
				log.Error().Err(err).Str("action", handler.ActionName).Msg("Failed to invoke rotary action")
			}
		})
	}

	if handler.CollectorFactory != nil {
		return handler.CollectorFactory.Create(onFlush)
	}
	return middleware.NewImmediateCollector(onFlush)
}

// registerLightChangeHandler sets up light change event handling via the event bus.
func registerLightChangeHandler(
	ctx context.Context,
	registry HandlerRegistry,
	bus *events.Bus,
	invoker *actions.Invoker,
	luaExec exec.Executor,
) {
	// Map from handler pointer to collector (each handler gets its own collector)
	collectors := make(map[*LightChangeHandler]middleware.Collector)
	var mu sync.Mutex

	bus.Subscribe(events.EventTypeLightChange, func(event events.Event) {
		resourceID, _ := event.Data["resource_id"].(string)
		resourceType, _ := event.Data["resource_type"].(string)

		handlers := registry.FindLightChangeHandlers(resourceID, resourceType)
		if len(handlers) == 0 {
			return
		}

		log.Debug().
			Str("resource_id", resourceID).
			Str("resource_type", resourceType).
			Int("handler_count", len(handlers)).
			Msg("Light change event matched handlers")

		// Dispatch to all matching handlers
		for _, handler := range handlers {
			mu.Lock()
			collector, ok := collectors[handler]
			if !ok {
				collector = createLightChangeCollector(ctx, handler, invoker, luaExec)
				collectors[handler] = collector
			}
			mu.Unlock()

			// Pass all event data to the collector
			collector.AddEvent(copyEventData(event.Data))
		}
	})
}

// copyEventData creates a copy of event data map
func copyEventData(data map[string]interface{}) map[string]any {
	result := make(map[string]any, len(data))
	for k, v := range data {
		result[k] = v
	}
	return result
}

// createLightChangeCollector creates a collector for light change events
func createLightChangeCollector(
	ctx context.Context,
	handler *LightChangeHandler,
	invoker *actions.Invoker,
	luaExec exec.Executor,
) middleware.Collector {
	onFlush := func(events []map[string]any) {
		luaExec.Do(ctx, func(workCtx context.Context) {
			var args map[string]any

			if handler.CollectorFactory != nil && handler.CollectorFactory.Reducer != nil {
				// Safe to call LState() here - we're inside Do() callback on Lua worker
				args = exec.CallReducer(luaExec.LState(), handler.CollectorFactory.Reducer, events)
			} else if len(events) > 0 {
				args = events[0]
			} else {
				args = make(map[string]any)
			}

			// Merge with static action args
			for k, v := range handler.ActionArgs {
				args[k] = v
			}

			if err := invoker.Invoke(workCtx, handler.ActionName, args, ""); err != nil {
				log.Error().Err(err).Str("action", handler.ActionName).Msg("Failed to invoke light change action")
			}
		})
	}

	if handler.CollectorFactory != nil {
		return handler.CollectorFactory.Create(onFlush)
	}
	return middleware.NewImmediateCollector(onFlush)
}
