package sse

import (
	"context"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/dokzlo13/lightd/internal/actions"
	"github.com/dokzlo13/lightd/internal/eventbus"
	"github.com/dokzlo13/lightd/internal/reconcile/group"
	"github.com/dokzlo13/lightd/internal/state"
)

// Rotary direction constants
const (
	RotaryDirectionClockwise        = "clock_wise"
	RotaryDirectionCounterClockwise = "counter_clock_wise"
)

// LuaExecutor provides thread-safe Lua execution
type LuaExecutor interface {
	Do(ctx context.Context, work func(ctx context.Context)) bool
}

// RegisterHandlers subscribes to SSE events on the event bus and dispatches to handlers
func (m *Module) RegisterHandlers(
	ctx context.Context,
	bus *eventbus.Bus,
	invoker *actions.Invoker,
	luaExec LuaExecutor,
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
	luaExec LuaExecutor,
	desiredStore *state.TypedStore[group.Desired],
) {
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

		// Capture handler values for closure
		h := handler
		eid := eventID

		// Queue work to Lua worker (single-threaded execution)
		luaExec.Do(ctx, func(workCtx context.Context) {
			if h.IsToggle {
				// Special handling for toggle buttons:
				// 1. Run init_bank first (no dedupe)
				// 2. Then run toggle_group (with dedupe)
				groupID, _ := h.ActionArgs["group"].(string)
				if groupID != "" {
					// Check if bank is set
					desired, _, _ := desiredStore.Get(groupID)
					if desired.SceneName == "" {
						invoker.Invoke(workCtx, "init_bank", map[string]any{"group": groupID}, "")
					}
				}
			}
			// Invoke action with button event ID as idempotency key
			invoker.Invoke(workCtx, h.ActionName, h.ActionArgs, eid)
		})
	})
}

// registerConnectivityHandler sets up connectivity event handling via the event bus.
func (m *Module) registerConnectivityHandler(
	ctx context.Context,
	bus *eventbus.Bus,
	invoker *actions.Invoker,
	luaExec LuaExecutor,
) {
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

		// Capture handler values for closure
		h := handler

		// Queue work to Lua worker (single-threaded execution)
		luaExec.Do(ctx, func(workCtx context.Context) {
			if err := invoker.Invoke(workCtx, h.ActionName, h.ActionArgs, ""); err != nil {
				log.Error().Err(err).Str("action", h.ActionName).Msg("Failed to invoke connectivity action")
			}
		})
	})
}

// registerRotaryHandler sets up rotary event handling via the event bus with debouncing.
func (m *Module) registerRotaryHandler(
	ctx context.Context,
	bus *eventbus.Bus,
	invoker *actions.Invoker,
	luaExec LuaExecutor,
) {
	// Create a debouncer per resource ID
	debouncers := make(map[string]*rotaryDebouncer)
	var mu sync.Mutex

	bus.Subscribe(eventbus.EventTypeRotary, func(event eventbus.Event) {
		resourceID, _ := event.Data["resource_id"].(string)
		direction, _ := event.Data["direction"].(string)
		steps, _ := event.Data["steps"].(int)

		handler := m.FindRotaryHandler(resourceID)
		if handler == nil {
			return
		}

		// Get or create debouncer for this resource
		mu.Lock()
		debouncer, ok := debouncers[resourceID]
		if !ok {
			debounceMs := handler.DebounceMs
			if debounceMs <= 0 {
				debounceMs = 50 // default
			}
			debouncer = &rotaryDebouncer{
				handler:      handler,
				invoker:      invoker,
				luaExec:      luaExec,
				ctx:          ctx,
				debounceTime: time.Duration(debounceMs) * time.Millisecond,
			}
			log.Debug().
				Str("resource_id", resourceID).
				Int("debounce_ms", debounceMs).
				Msg("Created rotary debouncer")
			debouncers[resourceID] = debouncer
		}
		mu.Unlock()

		// Add event to debouncer
		debouncer.addEvent(direction, steps)
	})
}

// rotaryDebouncer accumulates rotary events and fires once after a quiet period.
type rotaryDebouncer struct {
	mu               sync.Mutex
	accumulatedSteps int
	timer            *time.Timer
	debounceTime     time.Duration
	handler          *RotaryHandler
	invoker          *actions.Invoker
	luaExec          LuaExecutor
	ctx              context.Context
}

func (d *rotaryDebouncer) addEvent(direction string, steps int) {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Accumulate raw steps with sign based on direction
	// Let Lua handle the brightness conversion
	if direction == RotaryDirectionCounterClockwise {
		steps = -steps
	}
	d.accumulatedSteps += steps

	// Reset/start timer - fire after debounce time of quiet
	if d.timer != nil {
		d.timer.Stop()
	}
	d.timer = time.AfterFunc(d.debounceTime, d.apply)
}

func (d *rotaryDebouncer) apply() {
	d.mu.Lock()
	steps := d.accumulatedSteps
	d.accumulatedSteps = 0
	d.mu.Unlock()

	if steps == 0 {
		return
	}

	// Determine direction from net steps
	direction := RotaryDirectionClockwise
	if steps < 0 {
		direction = RotaryDirectionCounterClockwise
		steps = -steps // steps should be positive for Lua
	}

	log.Debug().
		Str("direction", direction).
		Int("net_steps", steps).
		Msg("Rotary debounced - applying")

	// Merge event data with handler args
	args := make(map[string]any)
	for k, v := range d.handler.ActionArgs {
		args[k] = v
	}
	args["direction"] = direction
	args["steps"] = steps // Raw steps, Lua handles conversion

	// Queue work to Lua worker
	actionName := d.handler.ActionName
	d.luaExec.Do(d.ctx, func(workCtx context.Context) {
		if err := d.invoker.Invoke(workCtx, actionName, args, ""); err != nil {
			log.Error().Err(err).Str("action", actionName).Msg("Failed to invoke rotary action")
		}
	})
}
