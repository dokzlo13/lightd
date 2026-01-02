// Package sse provides Lua bindings for Hue SSE (Server-Sent Events) event handlers.
// This includes button, rotary, and connectivity events from Hue devices.
package sse

import (
	lua "github.com/yuin/gopher-lua"

	"github.com/dokzlo13/lightd/internal/lua/modules"
	"github.com/dokzlo13/lightd/internal/lua/modules/collect"
)

// ButtonHandler is called when a button event occurs
type ButtonHandler struct {
	ResourceID       string
	ButtonAction     string
	ActionName       string
	ActionArgs       map[string]any
	IsToggle         bool                      // For button_toggle, special handling
	CollectorFactory *collect.CollectorFactory // nil = immediate
}

// ConnectivityHandler is called when connectivity changes
type ConnectivityHandler struct {
	DeviceID         string
	Status           string
	ActionName       string
	ActionArgs       map[string]any
	CollectorFactory *collect.CollectorFactory // nil = immediate
}

// RotaryHandler is called when a rotary event occurs
type RotaryHandler struct {
	ResourceID       string
	ActionName       string
	ActionArgs       map[string]any
	CollectorFactory *collect.CollectorFactory // nil = immediate
}

// Module provides events.sse Lua module for SSE event handlers
type Module struct {
	enabled              bool
	buttonHandlers       []ButtonHandler
	connectivityHandlers []ConnectivityHandler
	rotaryHandlers       []RotaryHandler
}

// NewModule creates a new SSE module
func NewModule(enabled bool) *Module {
	return &Module{
		enabled: enabled,
	}
}

// Loader is the module loader for Lua
func (m *Module) Loader(L *lua.LState) int {
	if !m.enabled {
		L.RaiseError("events.sse module is disabled (sse.enabled: false in config)")
		return 0
	}

	mod := L.NewTable()

	L.SetField(mod, "button", L.NewFunction(m.button))
	L.SetField(mod, "button_toggle", L.NewFunction(m.buttonToggle))
	L.SetField(mod, "connectivity", L.NewFunction(m.connectivity))
	L.SetField(mod, "rotary", L.NewFunction(m.rotary))

	L.Push(mod)
	return 1
}

// button(resource_id, button_action, action_name, args) - Register a button handler
func (m *Module) button(L *lua.LState) int {
	resourceID := L.CheckString(1)
	buttonAction := L.CheckString(2)
	actionName := L.CheckString(3)
	argsTable := L.OptTable(4, L.NewTable())

	args := modules.LuaTableToMap(argsTable)

	m.buttonHandlers = append(m.buttonHandlers, ButtonHandler{
		ResourceID:   resourceID,
		ButtonAction: buttonAction,
		ActionName:   actionName,
		ActionArgs:   args,
		IsToggle:     false,
	})

	return 0
}

// button_toggle(resource_id, button_action, args) - Register a toggle button handler
// This automatically handles bank initialization before toggle
func (m *Module) buttonToggle(L *lua.LState) int {
	resourceID := L.CheckString(1)
	buttonAction := L.CheckString(2)
	argsTable := L.OptTable(3, L.NewTable())

	args := modules.LuaTableToMap(argsTable)

	m.buttonHandlers = append(m.buttonHandlers, ButtonHandler{
		ResourceID:   resourceID,
		ButtonAction: buttonAction,
		ActionName:   "toggle_group", // Always toggle_group for toggle buttons
		ActionArgs:   args,
		IsToggle:     true,
	})

	return 0
}

// connectivity(device_id, status, action_name, args) - Register a connectivity handler
func (m *Module) connectivity(L *lua.LState) int {
	deviceID := L.CheckString(1)
	status := L.CheckString(2)
	actionName := L.CheckString(3)
	argsTable := L.OptTable(4, L.NewTable())

	args := modules.LuaTableToMap(argsTable)

	m.connectivityHandlers = append(m.connectivityHandlers, ConnectivityHandler{
		DeviceID:   deviceID,
		Status:     status,
		ActionName: actionName,
		ActionArgs: args,
	})

	return 0
}

// rotary(resource_id, action_name, args) - Register a rotary handler
// The action will receive direction and steps in args
// Optional args.middleware sets the collector middleware
func (m *Module) rotary(L *lua.LState) int {
	resourceID := L.CheckString(1)
	actionName := L.CheckString(2)
	argsTable := L.OptTable(3, L.NewTable())

	args := modules.LuaTableToMap(argsTable)

	// Extract collector factory from middleware field
	var factory *collect.CollectorFactory
	if mw := argsTable.RawGetString("middleware"); mw != lua.LNil {
		factory = collect.ExtractFactory(mw)
		delete(args, "middleware")
	}

	m.rotaryHandlers = append(m.rotaryHandlers, RotaryHandler{
		ResourceID:       resourceID,
		ActionName:       actionName,
		ActionArgs:       args,
		CollectorFactory: factory,
	})

	return 0
}

// GetButtonHandlers returns all registered button handlers
func (m *Module) GetButtonHandlers() []ButtonHandler {
	return m.buttonHandlers
}

// GetConnectivityHandlers returns all registered connectivity handlers
func (m *Module) GetConnectivityHandlers() []ConnectivityHandler {
	return m.connectivityHandlers
}

// GetRotaryHandlers returns all registered rotary handlers
func (m *Module) GetRotaryHandlers() []RotaryHandler {
	return m.rotaryHandlers
}

// FindButtonHandler finds a handler for a button event
func (m *Module) FindButtonHandler(resourceID, buttonAction string) *ButtonHandler {
	for i := range m.buttonHandlers {
		h := &m.buttonHandlers[i]
		if h.ResourceID == resourceID && h.ButtonAction == buttonAction {
			return h
		}
	}
	return nil
}

// FindConnectivityHandler finds a handler for a connectivity event
func (m *Module) FindConnectivityHandler(deviceID, status string) *ConnectivityHandler {
	for i := range m.connectivityHandlers {
		h := &m.connectivityHandlers[i]
		if h.DeviceID == deviceID && h.Status == status {
			return h
		}
	}
	return nil
}

// FindRotaryHandler finds a handler for a rotary event
func (m *Module) FindRotaryHandler(resourceID string) *RotaryHandler {
	for i := range m.rotaryHandlers {
		h := &m.rotaryHandlers[i]
		if h.ResourceID == resourceID {
			return h
		}
	}
	return nil
}
