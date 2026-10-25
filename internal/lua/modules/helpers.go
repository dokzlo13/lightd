package modules

import (
	"fmt"
	"github.com/dokzlo13/lightd/internal/lua/exec"

	lua "github.com/yuin/gopher-lua"
)

// LuaToGo converts a Lua value to a Go value
func CheckedLuaToGo(v lua.LValue) (any, error) { return exec.ToGo(v) }

type invalidLuaValue struct{ err error }

func (v invalidLuaValue) MarshalJSON() ([]byte, error) { return nil, v.err }
func LuaToGo(v lua.LValue) any {
	out, err := CheckedLuaToGo(v)
	if err != nil {
		return invalidLuaValue{err}
	}
	return out
}

// GoToLuaValue converts a Go value to a Lua value
func GoToLuaValue(L *lua.LState, v interface{}) lua.LValue {
	switch val := v.(type) {
	case nil:
		return lua.LNil
	case bool:
		return lua.LBool(val)
	case int:
		return lua.LNumber(val)
	case int64:
		return lua.LNumber(val)
	case float64:
		return lua.LNumber(val)
	case string:
		return lua.LString(val)
	case []interface{}:
		tbl := L.NewTable()
		for i, item := range val {
			tbl.RawSetInt(i+1, GoToLuaValue(L, item))
		}
		return tbl
	case map[string]string:
		tbl := L.NewTable()
		for k, v := range val {
			tbl.RawSetString(k, GoToLuaValue(L, v))
		}
		return tbl
	case []string:
		tbl := L.NewTable()
		for i, v := range val {
			tbl.RawSetInt(i+1, GoToLuaValue(L, v))
		}
		return tbl
	case map[string]interface{}:
		tbl := L.NewTable()
		for k, v := range val {
			tbl.RawSetString(k, GoToLuaValue(L, v))
		}
		return tbl
	default:
		return lua.LString(fmt.Sprintf("%v", v))
	}
}

// MapToLuaTable converts a Go map to a Lua table
func MapToLuaTable(L *lua.LState, m map[string]any) *lua.LTable {
	tbl := L.NewTable()
	for k, v := range m {
		L.SetField(tbl, k, GoToLuaValue(L, v))
	}
	return tbl
}

// LuaTableToMap converts a Lua table to a Go map
func LuaTableToMap(tbl *lua.LTable) map[string]any {
	m := make(map[string]any)
	tbl.ForEach(func(k, v lua.LValue) {
		if ks, ok := k.(lua.LString); ok {
			m[string(ks)] = LuaToGo(v)
		}
	})
	return m
}

func RequireLuaMap(L *lua.LState, t *lua.LTable) map[string]any {
	v, err := CheckedLuaToGo(t)
	if err != nil {
		L.RaiseError("invalid arguments: %s", err)
		return nil
	}
	m, ok := v.(map[string]any)
	if !ok {
		L.RaiseError("arguments must be an object table")
		return nil
	}
	return m
}

// Middleware is a runtime capability, not serializable action arguments.
func RequireEventArgs(L *lua.LState, t *lua.LTable) map[string]any {
	copy := L.NewTable()
	t.ForEach(func(k, v lua.LValue) {
		if k != lua.LString("middleware") {
			copy.RawSet(k, v)
		}
	})
	return RequireLuaMap(L, copy)
}
