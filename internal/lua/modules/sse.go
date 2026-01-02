// Package modules provides Lua module bindings.
package modules

import (
	glua "github.com/yuin/gopher-lua"

	"github.com/dokzlo13/lightd/internal/events/sse"
	"github.com/dokzlo13/lightd/internal/lua/modules/collect"
)

// SSEModule provides events.sse Lua module for SSE event handlers
type SSEModule struct {
	enabled              bool
	buttonHandlers       []sse.ButtonHandler
	connectivityHandlers []sse.ConnectivityHandler
	rotaryHandlers       []sse.RotaryHandler
	lightChangeHandlers  []sse.LightChangeHandler
}

// NewSSEModule creates a new SSE module
func NewSSEModule(enabled bool) *SSEModule {
	return &SSEModule{
		enabled: enabled,
	}
}

// Loader is the module loader for Lua
func (m *SSEModule) Loader(L *glua.LState) int {
	if !m.enabled {
		L.RaiseError("events.sse module is disabled (sse.enabled: false in config)")
		return 0
	}

	mod := L.NewTable()

	L.SetField(mod, "button", L.NewFunction(m.button))
	L.SetField(mod, "connectivity", L.NewFunction(m.connectivity))
	L.SetField(mod, "rotary", L.NewFunction(m.rotary))
	L.SetField(mod, "light_change", L.NewFunction(m.lightChange))

	L.Push(mod)
	return 1
}

// button(resource_id, button_action, action_name, args) - Register a button handler
// Optional args.middleware sets the collector middleware (e.g., collect.quiet for multi-click detection)
func (m *SSEModule) button(L *glua.LState) int {
	resourceID := L.CheckString(1)
	buttonAction := L.CheckString(2)
	actionName := L.CheckString(3)
	argsTable := L.OptTable(4, L.NewTable())

	args := LuaTableToMap(argsTable)

	// Extract collector factory from middleware field
	var factory *collect.CollectorFactory
	if mw := argsTable.RawGetString("middleware"); mw != glua.LNil {
		factory = collect.ExtractFactory(mw)
		delete(args, "middleware")
	}

	m.buttonHandlers = append(m.buttonHandlers, sse.ButtonHandler{
		ResourceID:       sse.ParseMatcher(resourceID),
		ButtonAction:     sse.ParseMatcher(buttonAction),
		ActionName:       actionName,
		ActionArgs:       args,
		CollectorFactory: factory,
	})

	return 0
}

// connectivity(device_id, status, action_name, args) - Register a connectivity handler
func (m *SSEModule) connectivity(L *glua.LState) int {
	deviceID := L.CheckString(1)
	status := L.CheckString(2)
	actionName := L.CheckString(3)
	argsTable := L.OptTable(4, L.NewTable())

	args := LuaTableToMap(argsTable)

	m.connectivityHandlers = append(m.connectivityHandlers, sse.ConnectivityHandler{
		DeviceID:   sse.ParseMatcher(deviceID),
		Status:     sse.ParseMatcher(status),
		ActionName: actionName,
		ActionArgs: args,
	})

	return 0
}

// rotary(resource_id, action_name, args) - Register a rotary handler
// The action will receive direction and steps in args
// Optional args.middleware sets the collector middleware
func (m *SSEModule) rotary(L *glua.LState) int {
	resourceID := L.CheckString(1)
	actionName := L.CheckString(2)
	argsTable := L.OptTable(3, L.NewTable())

	args := LuaTableToMap(argsTable)

	// Extract collector factory from middleware field
	var factory *collect.CollectorFactory
	if mw := argsTable.RawGetString("middleware"); mw != glua.LNil {
		factory = collect.ExtractFactory(mw)
		delete(args, "middleware")
	}

	m.rotaryHandlers = append(m.rotaryHandlers, sse.RotaryHandler{
		ResourceID:       sse.ParseMatcher(resourceID),
		ActionName:       actionName,
		ActionArgs:       args,
		CollectorFactory: factory,
	})

	return 0
}

// light_change(resource_id, action_name, args) - Register a light change handler
// resource_id: Light resource ID, "*" for all, or "id1|id2" for multiple
// Optional args.resource_type: "light", "grouped_light", "*" (default), or "light|grouped_light"
// Optional args.middleware sets the collector middleware
// The action will receive: resource_id, resource_type, brightness, power, color_temp_mirek, etc.
func (m *SSEModule) lightChange(L *glua.LState) int {
	resourceIDPattern := L.CheckString(1)
	actionName := L.CheckString(2)
	argsTable := L.OptTable(3, L.NewTable())

	args := LuaTableToMap(argsTable)

	// Extract resource_type filter (default "*" = all)
	resourceTypePattern := "*"
	if rt, ok := args["resource_type"].(string); ok {
		resourceTypePattern = rt
		delete(args, "resource_type")
	}

	// Extract collector factory from middleware field
	var factory *collect.CollectorFactory
	if mw := argsTable.RawGetString("middleware"); mw != glua.LNil {
		factory = collect.ExtractFactory(mw)
		delete(args, "middleware")
	}

	m.lightChangeHandlers = append(m.lightChangeHandlers, sse.LightChangeHandler{
		ResourceID:       sse.ParseMatcher(resourceIDPattern),
		ResourceType:     sse.ParseMatcher(resourceTypePattern),
		ActionName:       actionName,
		ActionArgs:       args,
		CollectorFactory: factory,
	})

	return 0
}

// GetButtonHandlers returns all registered button handlers
func (m *SSEModule) GetButtonHandlers() []sse.ButtonHandler {
	return m.buttonHandlers
}

// GetConnectivityHandlers returns all registered connectivity handlers
func (m *SSEModule) GetConnectivityHandlers() []sse.ConnectivityHandler {
	return m.connectivityHandlers
}

// GetRotaryHandlers returns all registered rotary handlers
func (m *SSEModule) GetRotaryHandlers() []sse.RotaryHandler {
	return m.rotaryHandlers
}

// FindButtonHandler finds a handler for a button event
func (m *SSEModule) FindButtonHandler(resourceID, buttonAction string) *sse.ButtonHandler {
	for i := range m.buttonHandlers {
		h := &m.buttonHandlers[i]
		if h.ResourceID.Matches(resourceID) && h.ButtonAction.Matches(buttonAction) {
			return h
		}
	}
	return nil
}

// FindConnectivityHandler finds a handler for a connectivity event
func (m *SSEModule) FindConnectivityHandler(deviceID, status string) *sse.ConnectivityHandler {
	for i := range m.connectivityHandlers {
		h := &m.connectivityHandlers[i]
		if h.DeviceID.Matches(deviceID) && h.Status.Matches(status) {
			return h
		}
	}
	return nil
}

// FindRotaryHandler finds a handler for a rotary event
func (m *SSEModule) FindRotaryHandler(resourceID string) *sse.RotaryHandler {
	for i := range m.rotaryHandlers {
		h := &m.rotaryHandlers[i]
		if h.ResourceID.Matches(resourceID) {
			return h
		}
	}
	return nil
}

// GetLightChangeHandlers returns all registered light change handlers
func (m *SSEModule) GetLightChangeHandlers() []sse.LightChangeHandler {
	return m.lightChangeHandlers
}

// FindLightChangeHandlers finds all handlers matching a light change event
// Returns multiple handlers since patterns can match multiple events
func (m *SSEModule) FindLightChangeHandlers(resourceID, resourceType string) []*sse.LightChangeHandler {
	var matches []*sse.LightChangeHandler
	for i := range m.lightChangeHandlers {
		h := &m.lightChangeHandlers[i]
		if h.ResourceID.Matches(resourceID) && h.ResourceType.Matches(resourceType) {
			matches = append(matches, h)
		}
	}
	return matches
}
