// Package exec provides the Executor interface for thread-safe Lua execution.
// This package is separate from lua to avoid import cycles with event handlers.
package exec

import (
	"context"
	"fmt"
	"math"

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
func CallReducer(L *glua.LState, reducer *glua.LFunction, events []map[string]any) (map[string]any, error) {
	// Convert events to Lua table array
	eventsTable := L.NewTable()
	for i, e := range events {
		eventTable := mapToLuaTable(L, e)
		eventsTable.RawSetInt(i+1, eventTable)
	}

	L.Push(reducer)
	L.Push(eventsTable)

	if err := L.PCall(1, 1, nil); err != nil {
		return nil, fmt.Errorf("Lua reducer: %w", err)
	}

	result := L.Get(-1)
	L.Pop(1)

	if tbl, ok := result.(*glua.LTable); ok {
		v, err := ToGo(tbl)
		if err != nil {
			return nil, err
		}
		m, ok := v.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("reducer must return an object table")
		}
		return m, nil
	}
	return nil, fmt.Errorf("reducer must return a table")
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
	case map[string]string:
		tbl := L.NewTable()
		for k, v := range val {
			tbl.RawSetString(k, goToLuaValue(L, v))
		}
		return tbl
	case []string:
		tbl := L.NewTable()
		for i, v := range val {
			tbl.RawSetInt(i+1, goToLuaValue(L, v))
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

// ToGo converts bounded acyclic JSON-shaped Lua values without unchecked indexing.
func ToGo(v glua.LValue) (any, error) { n := 0; return toGo(v, map[*glua.LTable]bool{}, 0, &n) }
func toGo(v glua.LValue, seen map[*glua.LTable]bool, depth int, n *int) (any, error) {
	*n++
	if *n > 4096 || depth > 32 {
		return nil, fmt.Errorf("Lua value exceeds conversion limit")
	}
	switch val := v.(type) {
	case glua.LString:
		return string(val), nil
	case glua.LNumber:
		f := float64(val)
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return nil, fmt.Errorf("Lua number must be finite")
		}
		return f, nil
	case glua.LBool:
		return bool(val), nil
	case *glua.LNilType:
		return nil, nil
	case *glua.LTable:
		if seen[val] {
			return nil, fmt.Errorf("cyclic Lua table")
		}
		seen[val] = true
		defer delete(seen, val)
		object := map[string]any{}
		array := map[int]any{}
		var convErr error
		val.ForEach(func(k, v glua.LValue) {
			if convErr != nil {
				return
			}
			item, err := toGo(v, seen, depth+1, n)
			if err != nil {
				convErr = err
				return
			}
			switch key := k.(type) {
			case glua.LString:
				object[string(key)] = item
			case glua.LNumber:
				i := int(key)
				if i < 1 || float64(i) != float64(key) || i > 4096 {
					convErr = fmt.Errorf("array keys must be positive contiguous integers")
					return
				}
				array[i] = item
			default:
				convErr = fmt.Errorf("unsupported Lua table key %s", k.Type())
			}
		})
		if convErr != nil {
			return nil, convErr
		}
		if len(array) == 0 {
			return object, nil
		}
		if len(object) > 0 {
			return nil, fmt.Errorf("mixed array and object table")
		}
		out := make([]any, len(array))
		for i := 1; i <= len(array); i++ {
			item, ok := array[i]
			if !ok {
				return nil, fmt.Errorf("sparse Lua array")
			}
			out[i-1] = item
		}
		return out, nil
	default:
		return nil, fmt.Errorf("unsupported Lua value %s", v.Type())
	}
}
func luaToGo(v glua.LValue) any {
	out, err := ToGo(v)
	if err != nil {
		return nil
	}
	return out
}
