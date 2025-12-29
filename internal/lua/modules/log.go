package modules

import (
	"github.com/rs/zerolog/log"
	lua "github.com/yuin/gopher-lua"
)

// LogModule provides logging functions to Lua
type LogModule struct{}

// NewLogModule creates a new log module
func NewLogModule() *LogModule {
	return &LogModule{}
}

// Loader is the module loader for Lua
func (m *LogModule) Loader(L *lua.LState) int {
	mod := L.NewTable()

	L.SetField(mod, "debug", L.NewFunction(m.debug))
	L.SetField(mod, "info", L.NewFunction(m.info))
	L.SetField(mod, "warn", L.NewFunction(m.warn))
	L.SetField(mod, "error", L.NewFunction(m.errorLog))

	L.Push(mod)
	return 1
}

func (m *LogModule) debug(L *lua.LState) int {
	msg := L.CheckString(1)
	fields := m.parseFields(L, 2)

	event := log.Debug().Str("source", "lua")
	for k, v := range fields {
		event = event.Interface(k, v)
	}
	event.Msg(msg)

	return 0
}

func (m *LogModule) info(L *lua.LState) int {
	msg := L.CheckString(1)
	fields := m.parseFields(L, 2)

	event := log.Info().Str("source", "lua")
	for k, v := range fields {
		event = event.Interface(k, v)
	}
	event.Msg(msg)

	return 0
}

func (m *LogModule) warn(L *lua.LState) int {
	msg := L.CheckString(1)
	fields := m.parseFields(L, 2)

	event := log.Warn().Str("source", "lua")
	for k, v := range fields {
		event = event.Interface(k, v)
	}
	event.Msg(msg)

	return 0
}

func (m *LogModule) errorLog(L *lua.LState) int {
	msg := L.CheckString(1)
	fields := m.parseFields(L, 2)

	event := log.Error().Str("source", "lua")
	for k, v := range fields {
		event = event.Interface(k, v)
	}
	event.Msg(msg)

	return 0
}

func (m *LogModule) parseFields(L *lua.LState, argIndex int) map[string]interface{} {
	fields := make(map[string]interface{})

	arg := L.Get(argIndex)
	if arg == lua.LNil {
		return fields
	}

	if tbl, ok := arg.(*lua.LTable); ok {
		tbl.ForEach(func(key, value lua.LValue) {
			keyStr := lua.LVAsString(key)
			fields[keyStr] = LuaToGo(value)
		})
	}

	return fields
}
