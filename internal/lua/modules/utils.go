package modules

import (
	"encoding/json"
	"math"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	lua "github.com/yuin/gopher-lua"
)

// UtilsModule provides utility functions to Lua
type UtilsModule struct{}

// NewUtilsModule creates a new utils module
func NewUtilsModule() *UtilsModule {
	return &UtilsModule{}
}

// Loader is the module loader for Lua
func (m *UtilsModule) Loader(L *lua.LState) int {
	mod := L.NewTable()

	// Time
	L.SetField(mod, "sleep", L.NewFunction(m.sleep))
	L.SetField(mod, "time_now", L.NewFunction(m.timeNow))
	L.SetField(mod, "time_ms", L.NewFunction(m.timeMs))
	L.SetField(mod, "time_format", L.NewFunction(m.timeFormat))

	// JSON
	L.SetField(mod, "json_encode", L.NewFunction(m.jsonEncode))
	L.SetField(mod, "json_decode", L.NewFunction(m.jsonDecode))

	// Math
	L.SetField(mod, "clamp", L.NewFunction(m.clamp))
	L.SetField(mod, "round", L.NewFunction(m.round))
	L.SetField(mod, "lerp", L.NewFunction(m.lerp))
	L.SetField(mod, "map_range", L.NewFunction(m.mapRange))

	// String
	L.SetField(mod, "split", L.NewFunction(m.split))
	L.SetField(mod, "trim", L.NewFunction(m.trim))
	L.SetField(mod, "join", L.NewFunction(m.join))
	L.SetField(mod, "starts_with", L.NewFunction(m.startsWith))
	L.SetField(mod, "ends_with", L.NewFunction(m.endsWith))

	// Table
	L.SetField(mod, "keys", L.NewFunction(m.keys))
	L.SetField(mod, "values", L.NewFunction(m.values))
	L.SetField(mod, "deep_copy", L.NewFunction(m.deepCopy))
	L.SetField(mod, "merge", L.NewFunction(m.merge))

	// Other
	L.SetField(mod, "uuid", L.NewFunction(m.uuidGen))
	L.SetField(mod, "env", L.NewFunction(m.env))

	L.Push(mod)
	return 1
}

// =============================================================================
// Time Functions
// =============================================================================

// sleep(ms) - Sleep for specified milliseconds
// This blocks the Lua execution but runs in Go's scheduler
func (m *UtilsModule) sleep(L *lua.LState) int {
	ms := L.CheckInt(1)
	time.Sleep(time.Duration(ms) * time.Millisecond)
	return 0
}

// time_now() -> number - Current Unix timestamp in seconds
func (m *UtilsModule) timeNow(L *lua.LState) int {
	L.Push(lua.LNumber(time.Now().Unix()))
	return 1
}

// time_ms() -> number - Current Unix timestamp in milliseconds
func (m *UtilsModule) timeMs(L *lua.LState) int {
	L.Push(lua.LNumber(time.Now().UnixMilli()))
	return 1
}

// time_format(timestamp, format) -> string
// Format: Go time format string (e.g., "2006-01-02 15:04:05", "15:04", "Mon Jan 2")
// If timestamp is 0 or nil, uses current time
func (m *UtilsModule) timeFormat(L *lua.LState) int {
	var ts time.Time

	arg1 := L.Get(1)
	if arg1 == lua.LNil {
		ts = time.Now()
	} else {
		tsUnix := L.CheckInt64(1)
		if tsUnix == 0 {
			ts = time.Now()
		} else {
			ts = time.Unix(tsUnix, 0)
		}
	}

	format := L.OptString(2, "2006-01-02 15:04:05")
	L.Push(lua.LString(ts.Format(format)))
	return 1
}

// =============================================================================
// JSON Functions
// =============================================================================

// json_encode(table) -> string, error
// Converts a Lua table to JSON string
func (m *UtilsModule) jsonEncode(L *lua.LState) int {
	value := L.Get(1)
	goValue := LuaToGo(value)

	jsonBytes, err := json.Marshal(goValue)
	if err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
	}

	L.Push(lua.LString(string(jsonBytes)))
	L.Push(lua.LNil)
	return 2
}

// json_decode(string) -> table, error
// Parses JSON string to Lua table
func (m *UtilsModule) jsonDecode(L *lua.LState) int {
	jsonStr := L.CheckString(1)

	var goValue interface{}
	if err := json.Unmarshal([]byte(jsonStr), &goValue); err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
	}

	L.Push(GoToLuaValue(L, goValue))
	L.Push(lua.LNil)
	return 2
}

// =============================================================================
// Math Functions
// =============================================================================

// clamp(value, min, max) -> number
// Clamps a value between min and max
func (m *UtilsModule) clamp(L *lua.LState) int {
	value := L.CheckNumber(1)
	minVal := L.CheckNumber(2)
	maxVal := L.CheckNumber(3)

	result := float64(value)
	if result < float64(minVal) {
		result = float64(minVal)
	}
	if result > float64(maxVal) {
		result = float64(maxVal)
	}

	L.Push(lua.LNumber(result))
	return 1
}

// round(value, decimals?) -> number
// Rounds a number to specified decimal places (default: 0)
func (m *UtilsModule) round(L *lua.LState) int {
	value := float64(L.CheckNumber(1))
	decimals := L.OptInt(2, 0)

	multiplier := math.Pow(10, float64(decimals))
	result := math.Round(value*multiplier) / multiplier

	L.Push(lua.LNumber(result))
	return 1
}

// lerp(a, b, t) -> number
// Linear interpolation between a and b by t (0-1)
func (m *UtilsModule) lerp(L *lua.LState) int {
	a := float64(L.CheckNumber(1))
	b := float64(L.CheckNumber(2))
	t := float64(L.CheckNumber(3))

	result := a + (b-a)*t
	L.Push(lua.LNumber(result))
	return 1
}

// map_range(value, in_min, in_max, out_min, out_max) -> number
// Maps a value from one range to another
func (m *UtilsModule) mapRange(L *lua.LState) int {
	value := float64(L.CheckNumber(1))
	inMin := float64(L.CheckNumber(2))
	inMax := float64(L.CheckNumber(3))
	outMin := float64(L.CheckNumber(4))
	outMax := float64(L.CheckNumber(5))

	// Avoid division by zero
	if inMax == inMin {
		L.Push(lua.LNumber(outMin))
		return 1
	}

	result := (value-inMin)*(outMax-outMin)/(inMax-inMin) + outMin
	L.Push(lua.LNumber(result))
	return 1
}

// =============================================================================
// String Functions
// =============================================================================

// split(str, sep) -> table
// Splits a string by separator
func (m *UtilsModule) split(L *lua.LState) int {
	str := L.CheckString(1)
	sep := L.OptString(2, ",")

	parts := strings.Split(str, sep)
	tbl := L.NewTable()
	for i, part := range parts {
		tbl.RawSetInt(i+1, lua.LString(part))
	}

	L.Push(tbl)
	return 1
}

// trim(str) -> string
// Removes leading and trailing whitespace
func (m *UtilsModule) trim(L *lua.LState) int {
	str := L.CheckString(1)
	L.Push(lua.LString(strings.TrimSpace(str)))
	return 1
}

// join(table, sep?) -> string
// Joins array elements with separator (default: ",")
func (m *UtilsModule) join(L *lua.LState) int {
	tbl := L.CheckTable(1)
	sep := L.OptString(2, ",")

	var parts []string
	tbl.ForEach(func(k, v lua.LValue) {
		if _, ok := k.(lua.LNumber); ok {
			parts = append(parts, lua.LVAsString(v))
		}
	})

	L.Push(lua.LString(strings.Join(parts, sep)))
	return 1
}

// starts_with(str, prefix) -> bool
func (m *UtilsModule) startsWith(L *lua.LState) int {
	str := L.CheckString(1)
	prefix := L.CheckString(2)
	L.Push(lua.LBool(strings.HasPrefix(str, prefix)))
	return 1
}

// ends_with(str, suffix) -> bool
func (m *UtilsModule) endsWith(L *lua.LState) int {
	str := L.CheckString(1)
	suffix := L.CheckString(2)
	L.Push(lua.LBool(strings.HasSuffix(str, suffix)))
	return 1
}

// =============================================================================
// Table Functions
// =============================================================================

// keys(table) -> table
// Returns array of table keys
func (m *UtilsModule) keys(L *lua.LState) int {
	tbl := L.CheckTable(1)
	result := L.NewTable()

	idx := 1
	tbl.ForEach(func(k, _ lua.LValue) {
		result.RawSetInt(idx, k)
		idx++
	})

	L.Push(result)
	return 1
}

// values(table) -> table
// Returns array of table values
func (m *UtilsModule) values(L *lua.LState) int {
	tbl := L.CheckTable(1)
	result := L.NewTable()

	idx := 1
	tbl.ForEach(func(_, v lua.LValue) {
		result.RawSetInt(idx, v)
		idx++
	})

	L.Push(result)
	return 1
}

// deep_copy(table) -> table
// Creates a deep copy of a table
func (m *UtilsModule) deepCopy(L *lua.LState) int {
	tbl := L.CheckTable(1)
	result := m.deepCopyTable(L, tbl)
	L.Push(result)
	return 1
}

func (m *UtilsModule) deepCopyTable(L *lua.LState, tbl *lua.LTable) *lua.LTable {
	result := L.NewTable()

	tbl.ForEach(func(k, v lua.LValue) {
		var newValue lua.LValue
		if innerTbl, ok := v.(*lua.LTable); ok {
			newValue = m.deepCopyTable(L, innerTbl)
		} else {
			newValue = v
		}
		result.RawSet(k, newValue)
	})

	return result
}

// merge(t1, t2) -> table
// Merges t2 into t1 (shallow), returns new table
// Values from t2 override t1
func (m *UtilsModule) merge(L *lua.LState) int {
	t1 := L.CheckTable(1)
	t2 := L.CheckTable(2)

	result := L.NewTable()

	// Copy t1
	t1.ForEach(func(k, v lua.LValue) {
		result.RawSet(k, v)
	})

	// Merge t2 (overrides t1)
	t2.ForEach(func(k, v lua.LValue) {
		result.RawSet(k, v)
	})

	L.Push(result)
	return 1
}

// =============================================================================
// Other Functions
// =============================================================================

// uuid() -> string
// Generates a new UUID v4
func (m *UtilsModule) uuidGen(L *lua.LState) int {
	L.Push(lua.LString(uuid.New().String()))
	return 1
}

// env(name) -> string | nil
// Gets an environment variable
func (m *UtilsModule) env(L *lua.LState) int {
	name := L.CheckString(1)
	value := os.Getenv(name)

	if value == "" {
		L.Push(lua.LNil)
	} else {
		L.Push(lua.LString(value))
	}
	return 1
}
