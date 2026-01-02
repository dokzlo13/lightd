// Package schedule provides event handling for scheduler events.
package schedule

import (
	"context"

	"github.com/rs/zerolog/log"

	"github.com/dokzlo13/lightd/internal/actions"
	"github.com/dokzlo13/lightd/internal/events"
	"github.com/dokzlo13/lightd/internal/lua/exec"
)

// RegisterHandler subscribes to schedule events on the event bus and dispatches to the invoker.
func RegisterHandler(
	ctx context.Context,
	bus *events.Bus,
	invoker *actions.Invoker,
	luaExec exec.Executor,
) {
	bus.Subscribe(events.EventTypeSchedule, func(event events.Event) {
		actionName, _ := event.Data["action_name"].(string)
		actionArgs, _ := event.Data["action_args"].(map[string]any)
		occurrenceID, _ := event.Data["occurrence_id"].(string)
		scheduleID, _ := event.Data["schedule_id"].(string)
		source, _ := event.Data["source"].(string)

		log.Debug().
			Str("schedule_id", scheduleID).
			Str("action", actionName).
			Str("occurrence_id", occurrenceID).
			Str("source", source).
			Msg("Schedule event received")

		// Capture values for closure
		aName := actionName
		aArgs := actionArgs
		occID := occurrenceID
		sID := scheduleID

		// Queue work to Lua worker (single-threaded execution)
		luaExec.Do(ctx, func(workCtx context.Context) {
			err := invoker.InvokeWithSource(workCtx, aName, aArgs, occID, "scheduler", sID)
			if err != nil {
				log.Error().Err(err).
					Str("action", aName).
					Str("schedule_id", sID).
					Str("occurrence_id", occID).
					Msg("Failed to invoke scheduled action")
			}
		})
	})
}
