package modules

import (
	"fmt"

	lua "github.com/yuin/gopher-lua"
)

// LuaToGo converts a Lua value to a Go value
func LuaToGo(v lua.LValue) interface{} {
	switch val := v.(type) {
	case lua.LString:
		return string(val)
	case lua.LNumber:
		return float64(val)
	case lua.LBool:
		return bool(val)
	case *lua.LTable:
		// Check if it's an array or object
		isArray := true
		maxIdx := 0
		val.ForEach(func(k, _ lua.LValue) {
			if num, ok := k.(lua.LNumber); ok {
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
			val.ForEach(func(k, v lua.LValue) {
				if num, ok := k.(lua.LNumber); ok {
					arr[int(num)-1] = LuaToGo(v)
				}
			})
			return arr
		}

		obj := make(map[string]interface{})
		val.ForEach(func(k, v lua.LValue) {
			obj[lua.LVAsString(k)] = LuaToGo(v)
		})
		return obj
	case *lua.LNilType:
		return nil
	default:
		return v.String()
	}
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

