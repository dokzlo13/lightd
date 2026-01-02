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

// MutableRegistry extends HandlerRegistry with change notification.
// When handlers are modified at runtime, the callback is invoked to invalidate caches.
type MutableRegistry interface {
	HandlerRegistry
	SetOnHandlersChanged(callback func())
}

// RegisterHandlers subscribes to SSE events on the event bus and dispatches to handlers.
// If the registry implements MutableRegistry, collectors are invalidated when handlers change.
func RegisterHandlers(
	ctx context.Context,
	registry HandlerRegistry,
	bus *events.Bus,
	invoker *actions.Invoker,
	luaExec exec.Executor,
) {
	// Collector caches for each event type
	buttonCollectors := &collectorCache{collectors: make(map[string]middleware.Collector)}
	connectivityCollectors := &collectorCache{collectors: make(map[string]middleware.Collector)}
	rotaryCollectors := &collectorCache{collectors: make(map[string]middleware.Collector)}
	lightChangeCollectors := &lightChangeCollectorCache{collectors: make(map[string]middleware.Collector)}

	// If registry supports change notification, set up invalidation
	if mutableReg, ok := registry.(MutableRegistry); ok {
		mutableReg.SetOnHandlersChanged(func() {
			log.Debug().Msg("Handlers changed, invalidating all collector caches")
			buttonCollectors.Clear()
			connectivityCollectors.Clear()
			rotaryCollectors.Clear()
			lightChangeCollectors.Clear()
		})
	}

	registerButtonHandler(ctx, registry, bus, invoker, luaExec, buttonCollectors)
	registerConnectivityHandler(ctx, registry, bus, invoker, luaExec, connectivityCollectors)
	registerRotaryHandler(ctx, registry, bus, invoker, luaExec, rotaryCollectors)
	registerLightChangeHandler(ctx, registry, bus, invoker, luaExec, lightChangeCollectors)
}

// collectorCache holds a thread-safe map of collectors that can be cleared
type collectorCache struct {
	mu         sync.Mutex
	collectors map[string]middleware.Collector
}

// Get returns a collector for the key, or nil if not found
func (c *collectorCache) Get(key string) (middleware.Collector, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	coll, ok := c.collectors[key]
	return coll, ok
}

// Set stores a collector for the key
func (c *collectorCache) Set(key string, coll middleware.Collector) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.collectors[key] = coll
}

// Clear closes all collectors and clears the cache
func (c *collectorCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	for key, coll := range c.collectors {
		coll.Close()
		delete(c.collectors, key)
	}
	log.Debug().Msg("Cleared collector cache")
}

// lightChangeCollectorCache holds collectors keyed by string (handler identity)
type lightChangeCollectorCache struct {
	mu         sync.Mutex
	collectors map[string]middleware.Collector
}

func (c *lightChangeCollectorCache) Get(key string) (middleware.Collector, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	coll, ok := c.collectors[key]
	return coll, ok
}

func (c *lightChangeCollectorCache) Set(key string, coll middleware.Collector) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.collectors[key] = coll
}

func (c *lightChangeCollectorCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	for key, coll := range c.collectors {
		coll.Close()
		delete(c.collectors, key)
	}
	log.Debug().Msg("Cleared light change collector cache")
}

// registerButtonHandler sets up button event handling via the event bus.
func registerButtonHandler(
	ctx context.Context,
	registry HandlerRegistry,
	bus *events.Bus,
	invoker *actions.Invoker,
	luaExec exec.Executor,
	cache *collectorCache,
) {
	bus.Subscribe(events.EventTypeButton, func(event events.Event) {
		resourceID, _ := event.Data["resource_id"].(string)
		buttonAction, _ := event.Data["action"].(string)
		eventID, _ := event.Data["event_id"].(string)

		handler := registry.FindButtonHandler(resourceID, buttonAction)
		if handler == nil {
			return
		}

		log.Info().
			Str("trigger", "button").
			Str("resource_id", resourceID).
			Str("button_action", buttonAction).
			Str("action", handler.ActionName).
			Msg("Action triggered by button press")

		// Build collector key
		collectorKey := resourceID + ":" + buttonAction

		collector, ok := cache.Get(collectorKey)
		if !ok {
			collector = createButtonCollector(ctx, handler, invoker, luaExec)
			cache.Set(collectorKey, collector)
		}

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
	cache *collectorCache,
) {
	bus.Subscribe(events.EventTypeConnectivity, func(event events.Event) {
		deviceID, _ := event.Data["device_id"].(string)
		status, _ := event.Data["status"].(string)

		handler := registry.FindConnectivityHandler(deviceID, status)
		if handler == nil {
			return
		}

		log.Info().
			Str("trigger", "connectivity").
			Str("device_id", deviceID).
			Str("status", status).
			Str("action", handler.ActionName).
			Msg("Action triggered by connectivity change")

		// Build collector key
		collectorKey := deviceID + ":" + status

		collector, ok := cache.Get(collectorKey)
		if !ok {
			collector = createConnectivityCollector(ctx, handler, invoker, luaExec)
			cache.Set(collectorKey, collector)
		}

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
	cache *collectorCache,
) {
	bus.Subscribe(events.EventTypeRotary, func(event events.Event) {
		resourceID, _ := event.Data["resource_id"].(string)
		direction, _ := event.Data["direction"].(string)
		steps, _ := event.Data["steps"].(int)

		handler := registry.FindRotaryHandler(resourceID)
		if handler == nil {
			return
		}

		log.Info().
			Str("trigger", "rotary").
			Str("resource_id", resourceID).
			Str("direction", direction).
			Int("steps", steps).
			Str("action", handler.ActionName).
			Msg("Action triggered by rotary dial")

		collector, ok := cache.Get(resourceID)
		if !ok {
			collector = createRotaryCollector(ctx, handler, invoker, luaExec)
			cache.Set(resourceID, collector)
		}

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
	cache *lightChangeCollectorCache,
) {
	bus.Subscribe(events.EventTypeLightChange, func(event events.Event) {
		resourceID, _ := event.Data["resource_id"].(string)
		resourceType, _ := event.Data["resource_type"].(string)

		handlers := registry.FindLightChangeHandlers(resourceID, resourceType)
		if len(handlers) == 0 {
			return
		}

		log.Info().
			Str("trigger", "light_change").
			Str("resource_id", resourceID).
			Str("resource_type", resourceType).
			Int("handler_count", len(handlers)).
			Msg("Action triggered by light change")

		// Dispatch to all matching handlers
		for _, handler := range handlers {
			// Use handler identity as key (action name + resource pattern)
			key := handler.ActionName + ":" + handler.ResourceID.String() + ":" + handler.ResourceType.String()

			collector, ok := cache.Get(key)
			if !ok {
				collector = createLightChangeCollector(ctx, handler, invoker, luaExec)
				cache.Set(key, collector)
			}

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
