package modules

import (
	"context"

	"github.com/rs/zerolog/log"
	lua "github.com/yuin/gopher-lua"

	"github.com/dokzlo13/lightd/internal/scheduler"
)

// SchedModule provides sched.define() and sched.run_closest() to Lua.
//
// ERROR HANDLING CONVENTION:
//   - define(), disable(), enable(): Use L.RaiseError() for critical setup failures
//   - run_closest(): Returns (ok, error_string) for runtime operations
type SchedModule struct {
	scheduler *scheduler.Scheduler
}

// NewSchedModule creates a new sched module
func NewSchedModule(sched *scheduler.Scheduler) *SchedModule {
	return &SchedModule{
		scheduler: sched,
	}
}

// Loader is the module loader for Lua
func (m *SchedModule) Loader(L *lua.LState) int {
	mod := L.NewTable()

	L.SetField(mod, "define", L.NewFunction(m.define))
	L.SetField(mod, "run_closest", L.NewFunction(m.runClosest))
	L.SetField(mod, "print", L.NewFunction(m.print))
	L.SetField(mod, "disable", L.NewFunction(m.disable))
	L.SetField(mod, "enable", L.NewFunction(m.enable))

	L.Push(mod)
	return 1
}

// define(id, time_expr, action_name, args, opts) - Register a schedule definition
func (m *SchedModule) define(L *lua.LState) int {
	id := L.CheckString(1)
	timeExpr := L.CheckString(2)
	actionName := L.CheckString(3)
	argsTable := L.OptTable(4, L.NewTable())
	optsTable := L.OptTable(5, L.NewTable())

	args := LuaTableToMap(argsTable)

	// Parse options
	tag := ""
	misfirePolicy := scheduler.MisfirePolicyRunLatest

	if t := optsTable.RawGetString("tag"); t != lua.LNil {
		tag = t.String()
	}
	if p := optsTable.RawGetString("misfire_policy"); p != lua.LNil {
		misfirePolicy = scheduler.MisfirePolicy(p.String())
	}

	if err := m.scheduler.Define(id, timeExpr, actionName, args, tag, misfirePolicy); err != nil {
		L.RaiseError("failed to define schedule: %s", err.Error())
		return 0
	}

	return 0
}

// run_closest(opts) -> (ok, err)
// Runs the closest schedule matching criteria. Uses NO idempotency key (always runs).
func (m *SchedModule) runClosest(L *lua.LState) int {
	optsTable := L.CheckTable(1)

	// Parse tags
	var tags []string
	if t := optsTable.RawGetString("tag"); t != lua.LNil {
		tags = append(tags, t.String())
	}
	if t := optsTable.RawGetString("tags"); t != lua.LNil {
		if tbl, ok := t.(*lua.LTable); ok {
			tbl.ForEach(func(_, v lua.LValue) {
				tags = append(tags, v.String())
			})
		}
	}

	// Parse strategy
	strategy := scheduler.StrategyNext
	if s := optsTable.RawGetString("strategy"); s != lua.LNil {
		strategy = scheduler.Strategy(s.String())
	}

	ctx := context.Background()
	if err := m.scheduler.RunClosest(ctx, tags, strategy); err != nil {
		log.Error().Err(err).
			Strs("tags", tags).
			Str("strategy", string(strategy)).
			Msg("Failed to run closest schedule")
		L.Push(lua.LBool(false))
		L.Push(lua.LString(err.Error()))
		return 2
	}

	L.Push(lua.LBool(true))
	L.Push(lua.LNil)
	return 2
}

// print() - Print the current schedule
func (m *SchedModule) print(L *lua.LState) int {
	schedule := m.scheduler.FormatSchedule()
	log.Info().Msg("Current schedule:\n" + schedule)
	return 0
}

// disable(id) - Disable a schedule definition
func (m *SchedModule) disable(L *lua.LState) int {
	id := L.CheckString(1)
	if err := m.scheduler.Disable(id); err != nil {
		L.RaiseError("failed to disable schedule: %s", err.Error())
	}
	return 0
}

// enable(id) - Enable a schedule definition
func (m *SchedModule) enable(L *lua.LState) int {
	id := L.CheckString(1)
	if err := m.scheduler.Enable(id); err != nil {
		L.RaiseError("failed to enable schedule: %s", err.Error())
	}
	return 0
}
