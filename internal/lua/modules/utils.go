package modules

import (
	"time"

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

	L.SetField(mod, "sleep", L.NewFunction(m.sleep))

	L.Push(mod)
	return 1
}

// sleep(ms) - Sleep for specified milliseconds
// This blocks the Lua execution but runs in Go's scheduler
func (m *UtilsModule) sleep(L *lua.LState) int {
	ms := L.CheckInt(1)
	time.Sleep(time.Duration(ms) * time.Millisecond)
	return 0
}

