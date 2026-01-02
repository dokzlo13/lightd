package sse

import (
	"context"
	"sync"

	"github.com/rs/zerolog/log"

	"github.com/dokzlo13/lightd/internal/actions"
	"github.com/dokzlo13/lightd/internal/eventbus"
	"github.com/dokzlo13/lightd/internal/luaexec"
	"github.com/dokzlo13/lightd/internal/middleware"
	"github.com/dokzlo13/lightd/internal/reconcile/group"
	"github.com/dokzlo13/lightd/internal/state"
)

// RegisterHandlers subscribes to SSE events on the event bus and dispatches to handlers
func (m *Module) RegisterHandlers(
	ctx context.Context,
	bus *eventbus.Bus,
	invoker *actions.Invoker,
	luaExec luaexec.Executor,
	desiredStore *state.TypedStore[group.Desired],
) {
	m.registerButtonHandler(ctx, bus, invoker, luaExec, desiredStore)
	m.registerConnectivityHandler(ctx, bus, invoker, luaExec)
	m.registerRotaryHandler(ctx, bus, invoker, luaExec)
}

// registerButtonHandler sets up button event handling via the event bus.
func (m *Module) registerButtonHandler(
	ctx context.Context,
	bus *eventbus.Bus,
	invoker *actions.Invoker,
	luaExec luaexec.Executor,
	desiredStore *state.TypedStore[group.Desired],
) {
	collectors := make(map[string]middleware.Collector)
	var mu sync.Mutex

	bus.Subscribe(eventbus.EventTypeButton, func(event eventbus.Event) {
		resourceID, _ := event.Data["resource_id"].(string)
		buttonAction, _ := event.Data["action"].(string)
		eventID, _ := event.Data["event_id"].(string)

		handler := m.FindButtonHandler(resourceID, buttonAction)
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
			collector = createButtonCollector(ctx, handler, invoker, luaExec, desiredStore)
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
	luaExec luaexec.Executor,
	desiredStore *state.TypedStore[group.Desired],
) middleware.Collector {
	onFlush := func(events []map[string]any) {
		luaExec.Do(ctx, func(workCtx context.Context) {
			var args map[string]any

			if handler.CollectorFactory != nil && handler.CollectorFactory.Reducer != nil {
				// Safe to call LState() here - we're inside Do() callback on Lua worker
				args = luaexec.CallReducer(luaExec.LState(), handler.CollectorFactory.Reducer, events)
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

			if handler.IsToggle {
				// Special handling for toggle buttons:
				// 1. Run init_bank first (no dedupe)
				// 2. Then run toggle_group (with dedupe)
				groupID, _ := handler.ActionArgs["group"].(string)
				if groupID != "" {
					// Check if bank is set
					desired, _, _ := desiredStore.Get(groupID)
					if desired.SceneName == "" {
						invoker.Invoke(workCtx, "init_bank", map[string]any{"group": groupID}, "")
					}
				}
			}
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
func (m *Module) registerConnectivityHandler(
	ctx context.Context,
	bus *eventbus.Bus,
	invoker *actions.Invoker,
	luaExec luaexec.Executor,
) {
	collectors := make(map[string]middleware.Collector)
	var mu sync.Mutex

	bus.Subscribe(eventbus.EventTypeConnectivity, func(event eventbus.Event) {
		deviceID, _ := event.Data["device_id"].(string)
		status, _ := event.Data["status"].(string)

		handler := m.FindConnectivityHandler(deviceID, status)
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
	luaExec luaexec.Executor,
) middleware.Collector {
	onFlush := func(events []map[string]any) {
		luaExec.Do(ctx, func(workCtx context.Context) {
			var args map[string]any

			if handler.CollectorFactory != nil && handler.CollectorFactory.Reducer != nil {
				// Safe to call LState() here - we're inside Do() callback on Lua worker
				args = luaexec.CallReducer(luaExec.LState(), handler.CollectorFactory.Reducer, events)
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
func (m *Module) registerRotaryHandler(
	ctx context.Context,
	bus *eventbus.Bus,
	invoker *actions.Invoker,
	luaExec luaexec.Executor,
) {
	collectors := make(map[string]middleware.Collector)
	var mu sync.Mutex

	bus.Subscribe(eventbus.EventTypeRotary, func(event eventbus.Event) {
		resourceID, _ := event.Data["resource_id"].(string)
		direction, _ := event.Data["direction"].(string)
		steps, _ := event.Data["steps"].(int)

		handler := m.FindRotaryHandler(resourceID)
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
	luaExec luaexec.Executor,
) middleware.Collector {
	onFlush := func(events []map[string]any) {
		luaExec.Do(ctx, func(workCtx context.Context) {
			var args map[string]any

			if handler.CollectorFactory != nil && handler.CollectorFactory.Reducer != nil {
				// Safe to call LState() here - we're inside Do() callback on Lua worker
				args = luaexec.CallReducer(luaExec.LState(), handler.CollectorFactory.Reducer, events)
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
