package modules

import (
	"time"

	"github.com/rs/zerolog/log"
	lua "github.com/yuin/gopher-lua"

	"github.com/dokzlo13/lightd/internal/scheduler"
)

// SchedModule provides sched.define(), sched.periodic(), sched.run_closest(),
// sched.list(), and sched.run() to Lua.
//
// ERROR HANDLING CONVENTION:
//   - define(), periodic(), disable(): Use L.RaiseError() for critical setup failures
//   - run_closest(), run(): Returns (ok, error_string) for runtime operations
type SchedModule struct {
	scheduler *scheduler.Scheduler
	enabled   bool
}

// NewSchedModule creates a new sched module
func NewSchedModule(sched *scheduler.Scheduler, enabled bool) *SchedModule {
	return &SchedModule{
		scheduler: sched,
		enabled:   enabled,
	}
}

// Loader is the module loader for Lua
func (m *SchedModule) Loader(L *lua.LState) int {
	if !m.enabled {
		L.RaiseError("sched module is disabled (scheduler.enabled: false in config)")
		return 0
	}

	mod := L.NewTable()

	L.SetField(mod, "define", L.NewFunction(m.define))
	L.SetField(mod, "periodic", L.NewFunction(m.periodic))
	L.SetField(mod, "run_closest", L.NewFunction(m.runClosest))
	L.SetField(mod, "print", L.NewFunction(m.print))
	L.SetField(mod, "disable", L.NewFunction(m.disable))

	// Primitives for cycling (logic implemented in Lua)
	L.SetField(mod, "list", L.NewFunction(m.list))
	L.SetField(mod, "get_closest", L.NewFunction(m.getClosest))
	L.SetField(mod, "run", L.NewFunction(m.run))

	L.Push(mod)
	return 1
}

// define(id, time_expr, action_name, args, opts) - Register a daily schedule definition
// opts.tag: optional tag for grouping schedules
// opts.replay: whether to replay on boot (default: true). Set to false to skip boot recovery.
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

	// Parse replay option: false or "never" means skip boot recovery
	if r := optsTable.RawGetString("replay"); r != lua.LNil {
		switch v := r.(type) {
		case lua.LBool:
			if !bool(v) {
				misfirePolicy = scheduler.MisfirePolicySkip
			}
		case lua.LString:
			if string(v) == "never" || string(v) == "false" {
				misfirePolicy = scheduler.MisfirePolicySkip
			}
		}
	}

	if err := m.scheduler.Define(id, timeExpr, actionName, args, tag, misfirePolicy); err != nil {
		L.RaiseError("failed to define schedule: %s", err.Error())
		return 0
	}

	return 0
}

// periodic(id, interval, action_name, args, opts) - Register a periodic schedule
// interval is a duration string like "30m", "1h", "5s"
func (m *SchedModule) periodic(L *lua.LState) int {
	id := L.CheckString(1)
	intervalStr := L.CheckString(2)
	actionName := L.CheckString(3)
	argsTable := L.OptTable(4, L.NewTable())
	optsTable := L.OptTable(5, L.NewTable())

	args := LuaTableToMap(argsTable)

	// Parse interval
	interval, err := time.ParseDuration(intervalStr)
	if err != nil {
		L.RaiseError("invalid interval %q: %s", intervalStr, err.Error())
		return 0
	}

	// Parse options
	tag := ""
	if t := optsTable.RawGetString("tag"); t != lua.LNil {
		tag = t.String()
	}

	m.scheduler.DefinePeriodic(id, interval, actionName, args, tag)

	log.Debug().
		Str("id", id).
		Dur("interval", interval).
		Str("action", actionName).
		Str("tag", tag).
		Msg("Periodic schedule registered")

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

	m.scheduler.RunClosest(tags, strategy)

	L.Push(lua.LBool(true))
	L.Push(lua.LNil)
	return 2
}

// list(opts) -> table of schedule IDs
// Returns schedules matching the tag, sorted by today's occurrence time.
// opts.tag: filter by tag (optional, returns all if not specified)
func (m *SchedModule) list(L *lua.LState) int {
	optsTable := L.OptTable(1, L.NewTable())

	tag := ""
	if t := optsTable.RawGetString("tag"); t != lua.LNil {
		tag = t.String()
	}

	schedules := m.scheduler.GetSchedulesByTag(tag)

	tbl := L.NewTable()
	for i, info := range schedules {
		tbl.RawSetInt(i+1, lua.LString(info.ID))
	}

	L.Push(tbl)
	return 1
}

// run(id) -> (ok, err)
// Runs a specific schedule by ID. Uses NO idempotency key (always runs).
func (m *SchedModule) run(L *lua.LState) int {
	id := L.CheckString(1)

	if err := m.scheduler.RunByID(id); err != nil {
		L.Push(lua.LFalse)
		L.Push(lua.LString(err.Error()))
		return 2
	}

	L.Push(lua.LTrue)
	L.Push(lua.LNil)
	return 2
}

// get_closest(opts) -> { id, action, tag, time } or nil
// Returns the closest schedule matching criteria without running it.
// opts.tag: filter by tag (optional)
// opts.strategy: "PREV" or "NEXT" (default: "PREV")
func (m *SchedModule) getClosest(L *lua.LState) int {
	optsTable := L.OptTable(1, L.NewTable())

	// Parse tag
	var tags []string
	if t := optsTable.RawGetString("tag"); t != lua.LNil {
		tags = append(tags, t.String())
	}

	// Parse strategy (default to PREV for finding current schedule)
	strategy := scheduler.StrategyPrev
	if s := optsTable.RawGetString("strategy"); s != lua.LNil {
		strategy = scheduler.Strategy(s.String())
	}

	info := m.scheduler.GetClosest(tags, strategy)
	if info == nil {
		L.Push(lua.LNil)
	} else {
		tbl := L.NewTable()
		L.SetField(tbl, "id", lua.LString(info.ID))
		L.SetField(tbl, "action", lua.LString(info.ActionName))
		L.SetField(tbl, "tag", lua.LString(info.Tag))
		L.SetField(tbl, "time", lua.LString(info.Time.Format("15:04:05")))
		L.Push(tbl)
	}
	return 1
}

// print(opts) - Print the current schedule
// opts.format: "today" (default) or "tomorrow"
func (m *SchedModule) print(L *lua.LState) int {
	day := time.Now()

	// Check for optional opts table
	if L.GetTop() >= 1 {
		if optsTable, ok := L.Get(1).(*lua.LTable); ok {
			if f := optsTable.RawGetString("format"); f != lua.LNil && f.String() == "tomorrow" {
				day = day.AddDate(0, 0, 1)
			}
		} else if L.Get(1).Type() == lua.LTString && L.Get(1).String() == "tomorrow" {
			day = day.AddDate(0, 0, 1)
		}
	}

	schedule := m.scheduler.FormatScheduleForDay(day)
	log.Info().Msg("Current schedule:\n" + schedule)
	return 0
}

// disable(id) - Disable/remove a schedule definition
func (m *SchedModule) disable(L *lua.LState) int {
	id := L.CheckString(1)
	if err := m.scheduler.Disable(id); err != nil {
		L.RaiseError("failed to disable schedule: %s", err.Error())
	}
	return 0
}
