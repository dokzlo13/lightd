// Package modules provides Lua module bindings.
package modules

import (
	"sync"

	"github.com/rs/zerolog/log"
	glua "github.com/yuin/gopher-lua"

	"github.com/dokzlo13/lightd/internal/events/sse"
	"github.com/dokzlo13/lightd/internal/lua/modules/collect"
)

// SSEModule provides events.sse Lua module for SSE event handlers.
// Supports dynamic bind/unbind of handlers at runtime.
type SSEModule struct {
	enabled bool

	mu                   sync.RWMutex // protects all handler slices
	buttonHandlers       []sse.ButtonHandler
	connectivityHandlers []sse.ConnectivityHandler
	rotaryHandlers       []sse.RotaryHandler
	lightChangeHandlers  []sse.LightChangeHandler

	onHandlersChanged func() // callback for collector invalidation
}

// NewSSEModule creates a new SSE module
func NewSSEModule(enabled bool) *SSEModule {
	return &SSEModule{
		enabled: enabled,
	}
}

// SetOnHandlersChanged sets the callback to invoke when handlers are modified.
// Used by the event dispatcher to invalidate cached collectors.
func (m *SSEModule) SetOnHandlersChanged(callback func()) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onHandlersChanged = callback
}

// notifyHandlersChanged calls the callback if set (must be called with lock held or after unlock)
func (m *SSEModule) notifyHandlersChanged() {
	if m.onHandlersChanged != nil {
		m.onHandlersChanged()
	}
}

// Loader is the module loader for Lua
func (m *SSEModule) Loader(L *glua.LState) int {
	if !m.enabled {
		L.RaiseError("events.sse module is disabled (sse.enabled: false in config)")
		return 0
	}

	mod := L.NewTable()

	// Registration functions
	L.SetField(mod, "button", L.NewFunction(m.button))
	L.SetField(mod, "connectivity", L.NewFunction(m.connectivity))
	L.SetField(mod, "rotary", L.NewFunction(m.rotary))
	L.SetField(mod, "light_change", L.NewFunction(m.lightChange))

	// Unbind functions
	L.SetField(mod, "unbind_button", L.NewFunction(m.unbindButton))
	L.SetField(mod, "unbind_connectivity", L.NewFunction(m.unbindConnectivity))
	L.SetField(mod, "unbind_rotary", L.NewFunction(m.unbindRotary))
	L.SetField(mod, "unbind_light_change", L.NewFunction(m.unbindLightChange))

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

	m.mu.Lock()
	m.buttonHandlers = append(m.buttonHandlers, sse.ButtonHandler{
		ResourceID:       sse.ParseMatcher(resourceID),
		ButtonAction:     sse.ParseMatcher(buttonAction),
		ActionName:       actionName,
		ActionArgs:       args,
		CollectorFactory: factory,
	})
	m.mu.Unlock()

	m.notifyHandlersChanged()

	log.Debug().
		Str("resource_id", resourceID).
		Str("button_action", buttonAction).
		Str("action", actionName).
		Msg("Registered button handler")

	return 0
}

// unbind_button(resource_id, button_action?) - Remove button handlers
// If button_action is omitted or "*", removes all handlers for the resource_id
func (m *SSEModule) unbindButton(L *glua.LState) int {
	resourceID := L.CheckString(1)
	buttonAction := L.OptString(2, "*")

	resourceMatcher := sse.ParseMatcher(resourceID)
	actionMatcher := sse.ParseMatcher(buttonAction)

	m.mu.Lock()
	original := len(m.buttonHandlers)
	filtered := m.buttonHandlers[:0]
	for _, h := range m.buttonHandlers {
		// Keep handlers that don't match the unbind criteria
		if !resourceMatcher.Matches(h.ResourceID.String()) ||
			!actionMatcher.Matches(h.ButtonAction.String()) {
			filtered = append(filtered, h)
		}
	}
	m.buttonHandlers = filtered
	removed := original - len(filtered)
	m.mu.Unlock()

	if removed > 0 {
		m.notifyHandlersChanged()
		log.Debug().
			Str("resource_id", resourceID).
			Str("button_action", buttonAction).
			Int("removed", removed).
			Msg("Unbound button handlers")
	}

	return 0
}

// connectivity(device_id, status, action_name, args) - Register a connectivity handler
func (m *SSEModule) connectivity(L *glua.LState) int {
	deviceID := L.CheckString(1)
	status := L.CheckString(2)
	actionName := L.CheckString(3)
	argsTable := L.OptTable(4, L.NewTable())

	args := LuaTableToMap(argsTable)

	// Extract collector factory from middleware field
	var factory *collect.CollectorFactory
	if mw := argsTable.RawGetString("middleware"); mw != glua.LNil {
		factory = collect.ExtractFactory(mw)
		delete(args, "middleware")
	}

	m.mu.Lock()
	m.connectivityHandlers = append(m.connectivityHandlers, sse.ConnectivityHandler{
		DeviceID:         sse.ParseMatcher(deviceID),
		Status:           sse.ParseMatcher(status),
		ActionName:       actionName,
		ActionArgs:       args,
		CollectorFactory: factory,
	})
	m.mu.Unlock()

	m.notifyHandlersChanged()

	log.Debug().
		Str("device_id", deviceID).
		Str("status", status).
		Str("action", actionName).
		Msg("Registered connectivity handler")

	return 0
}

// unbind_connectivity(device_id, status?) - Remove connectivity handlers
// If status is omitted or "*", removes all handlers for the device_id
func (m *SSEModule) unbindConnectivity(L *glua.LState) int {
	deviceID := L.CheckString(1)
	status := L.OptString(2, "*")

	deviceMatcher := sse.ParseMatcher(deviceID)
	statusMatcher := sse.ParseMatcher(status)

	m.mu.Lock()
	original := len(m.connectivityHandlers)
	filtered := m.connectivityHandlers[:0]
	for _, h := range m.connectivityHandlers {
		if !deviceMatcher.Matches(h.DeviceID.String()) ||
			!statusMatcher.Matches(h.Status.String()) {
			filtered = append(filtered, h)
		}
	}
	m.connectivityHandlers = filtered
	removed := original - len(filtered)
	m.mu.Unlock()

	if removed > 0 {
		m.notifyHandlersChanged()
		log.Debug().
			Str("device_id", deviceID).
			Str("status", status).
			Int("removed", removed).
			Msg("Unbound connectivity handlers")
	}

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

	m.mu.Lock()
	m.rotaryHandlers = append(m.rotaryHandlers, sse.RotaryHandler{
		ResourceID:       sse.ParseMatcher(resourceID),
		ActionName:       actionName,
		ActionArgs:       args,
		CollectorFactory: factory,
	})
	m.mu.Unlock()

	m.notifyHandlersChanged()

	log.Debug().
		Str("resource_id", resourceID).
		Str("action", actionName).
		Msg("Registered rotary handler")

	return 0
}

// unbind_rotary(resource_id) - Remove rotary handlers for the resource_id
func (m *SSEModule) unbindRotary(L *glua.LState) int {
	resourceID := L.CheckString(1)

	resourceMatcher := sse.ParseMatcher(resourceID)

	m.mu.Lock()
	original := len(m.rotaryHandlers)
	filtered := m.rotaryHandlers[:0]
	for _, h := range m.rotaryHandlers {
		if !resourceMatcher.Matches(h.ResourceID.String()) {
			filtered = append(filtered, h)
		}
	}
	m.rotaryHandlers = filtered
	removed := original - len(filtered)
	m.mu.Unlock()

	if removed > 0 {
		m.notifyHandlersChanged()
		log.Debug().
			Str("resource_id", resourceID).
			Int("removed", removed).
			Msg("Unbound rotary handlers")
	}

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

	m.mu.Lock()
	m.lightChangeHandlers = append(m.lightChangeHandlers, sse.LightChangeHandler{
		ResourceID:       sse.ParseMatcher(resourceIDPattern),
		ResourceType:     sse.ParseMatcher(resourceTypePattern),
		ActionName:       actionName,
		ActionArgs:       args,
		CollectorFactory: factory,
	})
	m.mu.Unlock()

	m.notifyHandlersChanged()

	log.Debug().
		Str("resource_id", resourceIDPattern).
		Str("resource_type", resourceTypePattern).
		Str("action", actionName).
		Msg("Registered light_change handler")

	return 0
}

// unbind_light_change(resource_id, resource_type?) - Remove light change handlers
// If resource_type is omitted or "*", removes all handlers for the resource_id
func (m *SSEModule) unbindLightChange(L *glua.LState) int {
	resourceID := L.CheckString(1)
	resourceType := L.OptString(2, "*")

	resourceMatcher := sse.ParseMatcher(resourceID)
	typeMatcher := sse.ParseMatcher(resourceType)

	m.mu.Lock()
	original := len(m.lightChangeHandlers)
	filtered := m.lightChangeHandlers[:0]
	for _, h := range m.lightChangeHandlers {
		if !resourceMatcher.Matches(h.ResourceID.String()) ||
			!typeMatcher.Matches(h.ResourceType.String()) {
			filtered = append(filtered, h)
		}
	}
	m.lightChangeHandlers = filtered
	removed := original - len(filtered)
	m.mu.Unlock()

	if removed > 0 {
		m.notifyHandlersChanged()
		log.Debug().
			Str("resource_id", resourceID).
			Str("resource_type", resourceType).
			Int("removed", removed).
			Msg("Unbound light_change handlers")
	}

	return 0
}

// GetButtonHandlers returns all registered button handlers
func (m *SSEModule) GetButtonHandlers() []sse.ButtonHandler {
	m.mu.RLock()
	defer m.mu.RUnlock()
	// Return a copy to prevent races
	result := make([]sse.ButtonHandler, len(m.buttonHandlers))
	copy(result, m.buttonHandlers)
	return result
}

// GetConnectivityHandlers returns all registered connectivity handlers
func (m *SSEModule) GetConnectivityHandlers() []sse.ConnectivityHandler {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]sse.ConnectivityHandler, len(m.connectivityHandlers))
	copy(result, m.connectivityHandlers)
	return result
}

// GetRotaryHandlers returns all registered rotary handlers
func (m *SSEModule) GetRotaryHandlers() []sse.RotaryHandler {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]sse.RotaryHandler, len(m.rotaryHandlers))
	copy(result, m.rotaryHandlers)
	return result
}

// GetLightChangeHandlers returns all registered light change handlers
func (m *SSEModule) GetLightChangeHandlers() []sse.LightChangeHandler {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]sse.LightChangeHandler, len(m.lightChangeHandlers))
	copy(result, m.lightChangeHandlers)
	return result
}

// FindButtonHandler finds a handler for a button event
func (m *SSEModule) FindButtonHandler(resourceID, buttonAction string) *sse.ButtonHandler {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for i := range m.buttonHandlers {
		h := &m.buttonHandlers[i]
		if h.ResourceID.Matches(resourceID) && h.ButtonAction.Matches(buttonAction) {
			// Return a copy to prevent races
			result := *h
			return &result
		}
	}
	return nil
}

// FindConnectivityHandler finds a handler for a connectivity event
func (m *SSEModule) FindConnectivityHandler(deviceID, status string) *sse.ConnectivityHandler {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for i := range m.connectivityHandlers {
		h := &m.connectivityHandlers[i]
		if h.DeviceID.Matches(deviceID) && h.Status.Matches(status) {
			result := *h
			return &result
		}
	}
	return nil
}

// FindRotaryHandler finds a handler for a rotary event
func (m *SSEModule) FindRotaryHandler(resourceID string) *sse.RotaryHandler {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for i := range m.rotaryHandlers {
		h := &m.rotaryHandlers[i]
		if h.ResourceID.Matches(resourceID) {
			result := *h
			return &result
		}
	}
	return nil
}

// FindLightChangeHandlers finds all handlers matching a light change event
// Returns multiple handlers since patterns can match multiple events
func (m *SSEModule) FindLightChangeHandlers(resourceID, resourceType string) []*sse.LightChangeHandler {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var matches []*sse.LightChangeHandler
	for i := range m.lightChangeHandlers {
		h := &m.lightChangeHandlers[i]
		if h.ResourceID.Matches(resourceID) && h.ResourceType.Matches(resourceType) {
			result := *h
			matches = append(matches, &result)
		}
	}
	return matches
}
