// Package luaexec provides shared interfaces for Lua execution.
package luaexec

import (
	"context"

	"github.com/rs/zerolog/log"
	lua "github.com/yuin/gopher-lua"

	"github.com/dokzlo13/lightd/internal/lua/modules"
)

// Executor provides thread-safe Lua execution and state access.
// This interface is implemented by LuaService and used by all event handlers.
type Executor interface {
	// Do queues work to be executed on the Lua VM
	Do(ctx context.Context, work func(ctx context.Context)) bool
	// LState returns the underlying Lua state (for use within Do callbacks only)
	LState() *lua.LState
}

// CallReducer calls a Lua reducer function with events array.
// MUST be called from within an Executor.Do() callback to ensure thread safety.
func CallReducer(L *lua.LState, reducer *lua.LFunction, events []map[string]any) map[string]any {
	// Convert events to Lua table array
	eventsTable := L.NewTable()
	for i, e := range events {
		eventTable := modules.MapToLuaTable(L, e)
		eventsTable.RawSetInt(i+1, eventTable)
	}

	L.Push(reducer)
	L.Push(eventsTable)

	if err := L.PCall(1, 1, nil); err != nil {
		log.Error().Err(err).Msg("Lua reducer failed")
		return make(map[string]any)
	}

	result := L.Get(-1)
	L.Pop(1)

	if tbl, ok := result.(*lua.LTable); ok {
		return modules.LuaTableToMap(tbl)
	}
	return make(map[string]any)
}
