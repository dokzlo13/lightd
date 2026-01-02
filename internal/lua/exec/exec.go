// Package exec provides the Executor interface for thread-safe Lua execution.
// This package is separate from lua to avoid import cycles with event handlers.
package exec

import (
	"context"
	"fmt"

	"github.com/rs/zerolog/log"
	glua "github.com/yuin/gopher-lua"
)

// Executor provides thread-safe Lua execution and state access.
// This interface is implemented by Runtime and used by all event handlers.
type Executor interface {
	// Do queues work to be executed on the Lua VM
	Do(ctx context.Context, work func(ctx context.Context)) bool
	// LState returns the underlying Lua state (for use within Do callbacks only)
	LState() *glua.LState
}

// CallReducer calls a Lua reducer function with events array.
// MUST be called from within an Executor.Do() callback to ensure thread safety.
func CallReducer(L *glua.LState, reducer *glua.LFunction, events []map[string]any) map[string]any {
	// Convert events to Lua table array
	eventsTable := L.NewTable()
	for i, e := range events {
		eventTable := mapToLuaTable(L, e)
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

	if tbl, ok := result.(*glua.LTable); ok {
		return luaTableToMap(tbl)
	}
	return make(map[string]any)
}

// mapToLuaTable converts a Go map to a Lua table
func mapToLuaTable(L *glua.LState, m map[string]any) *glua.LTable {
	tbl := L.NewTable()
	for k, v := range m {
		L.SetField(tbl, k, goToLuaValue(L, v))
	}
	return tbl
}

// luaTableToMap converts a Lua table to a Go map
func luaTableToMap(tbl *glua.LTable) map[string]any {
	m := make(map[string]any)
	tbl.ForEach(func(k, v glua.LValue) {
		if ks, ok := k.(glua.LString); ok {
			m[string(ks)] = luaToGo(v)
		}
	})
	return m
}

// goToLuaValue converts a Go value to a Lua value
func goToLuaValue(L *glua.LState, v interface{}) glua.LValue {
	switch val := v.(type) {
	case nil:
		return glua.LNil
	case bool:
		return glua.LBool(val)
	case int:
		return glua.LNumber(val)
	case int64:
		return glua.LNumber(val)
	case float64:
		return glua.LNumber(val)
	case string:
		return glua.LString(val)
	case []interface{}:
		tbl := L.NewTable()
		for i, item := range val {
			tbl.RawSetInt(i+1, goToLuaValue(L, item))
		}
		return tbl
	case map[string]interface{}:
		tbl := L.NewTable()
		for k, v := range val {
			tbl.RawSetString(k, goToLuaValue(L, v))
		}
		return tbl
	default:
		return glua.LString(fmt.Sprintf("%v", v))
	}
}

// luaToGo converts a Lua value to a Go value
func luaToGo(v glua.LValue) interface{} {
	switch val := v.(type) {
	case glua.LString:
		return string(val)
	case glua.LNumber:
		return float64(val)
	case glua.LBool:
		return bool(val)
	case *glua.LTable:
		// Check if it's an array or object
		isArray := true
		maxIdx := 0
		val.ForEach(func(k, _ glua.LValue) {
			if num, ok := k.(glua.LNumber); ok {
				idx := int(num)
				if idx > maxIdx {
					maxIdx = idx
				}
			} else {
				isArray = false
			}
		})

		if isArray && maxIdx > 0 {
			arr := make([]interface{}, maxIdx)
			val.ForEach(func(k, v glua.LValue) {
				if num, ok := k.(glua.LNumber); ok {
					arr[int(num)-1] = luaToGo(v)
				}
			})
			return arr
		}

		obj := make(map[string]interface{})
		val.ForEach(func(k, v glua.LValue) {
			obj[glua.LVAsString(k)] = luaToGo(v)
		})
		return obj
	case *glua.LNilType:
		return nil
	default:
		return v.String()
	}
}
