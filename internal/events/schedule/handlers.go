// Package schedule admits scheduler events to the serial Lua worker.
package schedule

import (
	"context"
	"fmt"
	"time"

	"github.com/dokzlo13/lightd/internal/actions"
	"github.com/dokzlo13/lightd/internal/events"
	"github.com/dokzlo13/lightd/internal/lua/exec"
)

func RegisterHandler(ctx context.Context, bus *events.Bus, invoker *actions.Invoker, luaExec exec.Executor) {
	bus.SubscribeAck(events.EventTypeSchedule, func(event events.Event) error {
		name, _ := event.Data["action_name"].(string)
		args, _ := event.Data["action_args"].(map[string]any)
		key, _ := event.Data["occurrence_id"].(string)
		id, _ := event.Data["schedule_id"].(string)
		source, _ := event.Data["source"].(string)
		at, _ := event.Data["run_at"].(time.Time)
		version, _ := event.Data["policy_version"].(string)
		generation, _ := event.Data["generation"].(int64)
		valid, _ := event.Data["generation_valid"].(func() bool)
		if name == "" || !invoker.HasAction(name) {
			return fmt.Errorf("schedule %q references unknown action %q", id, name)
		}
		workCtx := actions.WithMetadata(ctx, actions.Metadata{PlannedAt: at, PolicyVersion: version, Generation: generation})
		return invoker.QueuePrepared(workCtx, luaExec, name, key, source, id, func(context.Context) (map[string]any, error) {
			if valid != nil && !valid() {
				return nil, fmt.Errorf("schedule %q superseded before execution", id)
			}
			return args, nil
		})
	})
}
