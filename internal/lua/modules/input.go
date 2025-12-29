package modules

import (
	lua "github.com/yuin/gopher-lua"
)

// ButtonHandler is called when a button event occurs
type ButtonHandler struct {
	ResourceID   string
	ButtonAction string
	ActionName   string
	ActionArgs   map[string]any
	IsToggle     bool // For button_toggle, special handling
}

// ConnectivityHandler is called when connectivity changes
type ConnectivityHandler struct {
	DeviceID   string
	Status     string
	ActionName string
	ActionArgs map[string]any
}

// RotaryHandler is called when a rotary event occurs
type RotaryHandler struct {
	ResourceID string
	ActionName string
	ActionArgs map[string]any
	DebounceMs int // Debounce time in milliseconds (default: 50)
}

// InputModule provides Input.button(), Input.connectivity(), and Input.rotary() to Lua
type InputModule struct {
	buttonHandlers       []ButtonHandler
	connectivityHandlers []ConnectivityHandler
	rotaryHandlers       []RotaryHandler
}

// NewInputModule creates a new input module
func NewInputModule() *InputModule {
	return &InputModule{}
}

// Loader is the module loader for Lua
func (m *InputModule) Loader(L *lua.LState) int {
	mod := L.NewTable()

	L.SetField(mod, "button", L.NewFunction(m.button))
	L.SetField(mod, "button_toggle", L.NewFunction(m.buttonToggle))
	L.SetField(mod, "connectivity", L.NewFunction(m.connectivity))
	L.SetField(mod, "rotary", L.NewFunction(m.rotary))

	L.Push(mod)
	return 1
}

// button(resource_id, button_action, action_name, args) - Register a button handler
func (m *InputModule) button(L *lua.LState) int {
	resourceID := L.CheckString(1)
	buttonAction := L.CheckString(2)
	actionName := L.CheckString(3)
	argsTable := L.OptTable(4, L.NewTable())

	args := LuaTableToMap(argsTable)

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
func (m *InputModule) buttonToggle(L *lua.LState) int {
	resourceID := L.CheckString(1)
	buttonAction := L.CheckString(2)
	argsTable := L.OptTable(3, L.NewTable())

	args := LuaTableToMap(argsTable)

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
func (m *InputModule) connectivity(L *lua.LState) int {
	deviceID := L.CheckString(1)
	status := L.CheckString(2)
	actionName := L.CheckString(3)
	argsTable := L.OptTable(4, L.NewTable())

	args := LuaTableToMap(argsTable)

	m.connectivityHandlers = append(m.connectivityHandlers, ConnectivityHandler{
		DeviceID:   deviceID,
		Status:     status,
		ActionName: actionName,
		ActionArgs: args,
	})

	return 0
}

// rotary(resource_id, action_name, args) - Register a rotary handler
// The action will receive direction, steps, and duration in args
// Optional args.debounce_ms sets the debounce time (default: 50ms)
func (m *InputModule) rotary(L *lua.LState) int {
	resourceID := L.CheckString(1)
	actionName := L.CheckString(2)
	argsTable := L.OptTable(3, L.NewTable())

	args := LuaTableToMap(argsTable)

	// Extract debounce_ms from args (default: 50)
	debounceMs := 50
	if v, ok := args["debounce_ms"]; ok {
		if f, ok := v.(float64); ok {
			debounceMs = int(f)
		}
		delete(args, "debounce_ms") // Don't pass to action
	}

	m.rotaryHandlers = append(m.rotaryHandlers, RotaryHandler{
		ResourceID: resourceID,
		ActionName: actionName,
		ActionArgs: args,
		DebounceMs: debounceMs,
	})

	return 0
}

// GetButtonHandlers returns all registered button handlers
func (m *InputModule) GetButtonHandlers() []ButtonHandler {
	return m.buttonHandlers
}

// GetConnectivityHandlers returns all registered connectivity handlers
func (m *InputModule) GetConnectivityHandlers() []ConnectivityHandler {
	return m.connectivityHandlers
}

// FindButtonHandler finds a handler for a button event
func (m *InputModule) FindButtonHandler(resourceID, buttonAction string) *ButtonHandler {
	for i := range m.buttonHandlers {
		h := &m.buttonHandlers[i]
		if h.ResourceID == resourceID && h.ButtonAction == buttonAction {
			return h
		}
	}
	return nil
}

// FindConnectivityHandler finds a handler for a connectivity event
func (m *InputModule) FindConnectivityHandler(deviceID, status string) *ConnectivityHandler {
	for i := range m.connectivityHandlers {
		h := &m.connectivityHandlers[i]
		if h.DeviceID == deviceID && h.Status == status {
			return h
		}
	}
	return nil
}

// GetRotaryHandlers returns all registered rotary handlers
func (m *InputModule) GetRotaryHandlers() []RotaryHandler {
	return m.rotaryHandlers
}

// FindRotaryHandler finds a handler for a rotary event
func (m *InputModule) FindRotaryHandler(resourceID string) *RotaryHandler {
	for i := range m.rotaryHandlers {
		h := &m.rotaryHandlers[i]
		if h.ResourceID == resourceID {
			return h
		}
	}
	return nil
}
