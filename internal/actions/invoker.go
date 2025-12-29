package actions

import (
	"context"
	"fmt"

	"github.com/rs/zerolog/log"

	"github.com/dokzlo13/lightd/internal/ledger"
)

// Invoker executes actions with deduplication and restart recovery
type Invoker struct {
	registry   *Registry
	ledger     *ledger.Ledger
	ctxFactory func(ctx context.Context) *Context
}

// NewInvoker creates a new action invoker
func NewInvoker(registry *Registry, l *ledger.Ledger, ctxFactory func(ctx context.Context) *Context) *Invoker {
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

	// Check for orphaned started entry (restart recovery)
	if idempotencyKey != "" {
		if started, err := i.ledger.GetStarted(idempotencyKey); err == nil && started != nil {
			log.Info().
				Str("action", actionName).
				Str("idempotency_key", idempotencyKey).
				Msg("Found orphaned action_started, replaying captured decision")
			return i.executeWithCapturedState(ctx, started)
		}
	}

	// Fresh execution
	action, exists := i.registry.Get(actionName)
	if !exists {
		return fmt.Errorf("action %q not found", actionName)
	}

	actx := i.ctxFactory(ctx)

	// Capture decision (for stateful actions this reads actual state)
	capturedState, err := action.CaptureDecision(actx, args)
	if err != nil {
		return fmt.Errorf("failed to capture decision: %w", err)
	}

	// Log action_started with captured decision
	if idempotencyKey != "" {
		payload := map[string]any{
			"action":         actionName,
			"args":           args,
			"captured_state": capturedState,
		}
		if err := i.appendLedger(ledger.EventActionStarted, idempotencyKey, source, defID, payload); err != nil {
			log.Error().Err(err).Msg("Failed to log action_started")
		}
	}

	// Execute with the captured decision
	logEvent := log.Debug().Str("action", actionName).Interface("args", args)
	if source != "" {
		logEvent = logEvent.Str("source", source)
	}
	logEvent.Msg("Executing action")

	err = action.Execute(actx, args, capturedState)

	// Log completion or failure
	if err != nil {
		if idempotencyKey != "" {
			i.appendLedger(ledger.EventActionFailed, idempotencyKey, source, defID, map[string]any{
				"action": actionName,
				"error":  err.Error(),
			})
		}
		return err
	}

	if idempotencyKey != "" {
		i.appendLedger(ledger.EventActionCompleted, idempotencyKey, source, defID, map[string]any{
			"action": actionName,
		})
	}

	return nil
}

// appendLedger appends to ledger, using source/defID if provided
func (i *Invoker) appendLedger(eventType ledger.EventType, idempotencyKey, source, defID string, payload map[string]any) error {
	if source != "" || defID != "" {
		return i.ledger.AppendWithSource(eventType, idempotencyKey, source, defID, payload)
	}
	return i.ledger.Append(eventType, idempotencyKey, payload)
}

// RecoverOrphanedActions recovers all actions that were started but not completed
// Called at startup before normal operation
func (i *Invoker) RecoverOrphanedActions(ctx context.Context) error {
	orphans, err := i.ledger.GetOrphanedStarts()
	if err != nil {
		return fmt.Errorf("failed to get orphaned starts: %w", err)
	}

	for _, started := range orphans {
		log.Info().
			Str("idempotency_key", started.IdempotencyKey).
			Interface("payload", started.Payload).
			Msg("Recovering orphaned action")

		if err := i.executeWithCapturedState(ctx, started); err != nil {
			log.Error().Err(err).
				Str("idempotency_key", started.IdempotencyKey).
				Msg("Failed to recover orphaned action")
			// Continue with other orphans
		}
	}

	return nil
}

func (i *Invoker) executeWithCapturedState(ctx context.Context, started *ledger.Entry) error {
	actionName, ok := started.Payload["action"].(string)
	if !ok {
		return fmt.Errorf("invalid action in payload")
	}

	args, _ := started.Payload["args"].(map[string]any)
	capturedState, _ := started.Payload["captured_state"].(map[string]any)

	action, exists := i.registry.Get(actionName)
	if !exists {
		return fmt.Errorf("action %q not found", actionName)
	}

	actx := i.ctxFactory(ctx)
	err := action.Execute(actx, args, capturedState)

	// Log completion
	if err != nil {
		i.ledger.Append(ledger.EventActionFailed, started.IdempotencyKey, map[string]any{
			"action": actionName,
			"error":  err.Error(),
		})
		return err
	}

	i.ledger.Append(ledger.EventActionCompleted, started.IdempotencyKey, map[string]any{
		"action": actionName,
	})

	return nil
}
