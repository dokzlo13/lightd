package actions

import (
	"context"
	"fmt"

	"github.com/rs/zerolog/log"

	"github.com/dokzlo13/lightd/internal/storage"
)

// Invoker executes actions with deduplication
type Invoker struct {
	registry   *Registry
	ledger     *storage.Ledger
	ctxFactory func(ctx context.Context) *Context
}

// NewInvoker creates a new action invoker
func NewInvoker(registry *Registry, l *storage.Ledger, ctxFactory func(ctx context.Context) *Context) *Invoker {
	return &Invoker{
		registry:   registry,
		ledger:     l,
		ctxFactory: ctxFactory,
	}
}

// Invoke executes an action with the given idempotency key
// - For schedules: idempotencyKey = occurrence_id ("scene:dawn/1735372800")
// - For buttons: idempotencyKey = button_event_id (from Hue SSE)
// - For manual/programmatic calls: idempotencyKey = "" (no dedupe)
func (i *Invoker) Invoke(ctx context.Context, actionName string, args map[string]any, idempotencyKey string) error {
	return i.invoke(ctx, actionName, args, idempotencyKey, "", "")
}

// InvokeWithSource is like Invoke but includes source and def_id for ledger tracking
func (i *Invoker) InvokeWithSource(ctx context.Context, actionName string, args map[string]any, idempotencyKey, source, defID string) error {
	return i.invoke(ctx, actionName, args, idempotencyKey, source, defID)
}

// HasAction checks if an action is registered
func (i *Invoker) HasAction(actionName string) bool {
	_, exists := i.registry.Get(actionName)
	return exists
}

// invoke is the shared implementation for Invoke and InvokeWithSource
func (i *Invoker) invoke(ctx context.Context, actionName string, args map[string]any, idempotencyKey, source, defID string) error {
	// Check if already completed (dedupe)
	if idempotencyKey != "" && i.ledger.HasCompleted(idempotencyKey) {
		log.Debug().
			Str("action", actionName).
			Str("idempotency_key", idempotencyKey).
			Msg("Action already completed, skipping")
		return nil
	}

	// Get action
	action, exists := i.registry.Get(actionName)
	if !exists {
		return fmt.Errorf("action %q not found", actionName)
	}

	actx := i.ctxFactory(ctx)

	// Execute action
	logEvent := log.Debug().Str("action", actionName).Interface("args", args)
	if source != "" {
		logEvent = logEvent.Str("source", source)
	}
	logEvent.Msg("Executing action")

	err := action.Execute(actx, args)

	// Log completion or failure
	if err != nil {
		if idempotencyKey != "" {
			i.appendLedger(storage.EventActionFailed, idempotencyKey, source, defID, map[string]any{
				"action": actionName,
				"error":  err.Error(),
			})
		}
		return err
	}

	if idempotencyKey != "" {
		i.appendLedger(storage.EventActionCompleted, idempotencyKey, source, defID, map[string]any{
			"action": actionName,
		})
	}

	return nil
}

// appendLedger appends to ledger, using source/defID if provided
func (i *Invoker) appendLedger(eventType storage.EventType, idempotencyKey, source, defID string, payload map[string]any) error {
	if source != "" || defID != "" {
		return i.ledger.AppendWithSource(eventType, idempotencyKey, source, defID, payload)
	}
	return i.ledger.Append(eventType, idempotencyKey, payload)
}
