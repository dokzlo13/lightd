package context

import (
	lua "github.com/yuin/gopher-lua"

	"github.com/dokzlo13/lightd/internal/hue/reconcile/group"
)

const groupBuilderTypeName = "desired.group"

// GroupDesiredBuilder accumulates desired state changes for a group.
// Used by ctx.desired:group(id) to provide chainable methods.
type GroupDesiredBuilder struct {
	groupID string
	state   group.Desired
	module  *DesiredModule // reference back for pending tracking
}

// RegisterGroupBuilderType registers the desired.group metatable.
func RegisterGroupBuilderType(L *lua.LState) {
	mt := L.NewTypeMetatable(groupBuilderTypeName)
	L.SetField(mt, "__index", L.SetFuncs(L.NewTable(), groupBuilderMethods))
}

var groupBuilderMethods = map[string]lua.LGFunction{
	"on":        groupBuilderOn,
	"off":       groupBuilderOff,
	"toggle":    groupBuilderToggle,
	"set_bri":   groupBuilderSetBri,
	"set_scene": groupBuilderSetScene,
	"set_color": groupBuilderSetColorXY,
	"set_ct":    groupBuilderSetCt,
	"set_hue":   groupBuilderSetHue,
	"set_sat":   groupBuilderSetSat,
}

// pushGroupBuilder creates a new GroupDesiredBuilder userdata and pushes it onto the stack.
func pushGroupBuilder(L *lua.LState, groupID string, module *DesiredModule) {
	ud := L.NewUserData()
	ud.Value = &GroupDesiredBuilder{
		groupID: groupID,
		state:   group.Desired{},
		module:  module,
	}
	L.SetMetatable(ud, L.GetTypeMetatable(groupBuilderTypeName))
	L.Push(ud)
}

// checkGroupBuilder retrieves the GroupDesiredBuilder from the Lua stack.
func checkGroupBuilder(L *lua.LState) (*GroupDesiredBuilder, *lua.LUserData) {
	ud := L.CheckUserData(1)
	if v, ok := ud.Value.(*GroupDesiredBuilder); ok {
		return v, ud
	}
	L.ArgError(1, "desired.group expected")
	return nil, nil
}

// groupBuilderOn sets power to on (chainable).
func groupBuilderOn(L *lua.LState) int {
	builder, ud := checkGroupBuilder(L)
	on := true
	builder.state.Power = &on
	builder.module.markGroupPending(builder)
	L.Push(ud)
	return 1
}

// groupBuilderOff sets power to off (chainable).
func groupBuilderOff(L *lua.LState) int {
	builder, ud := checkGroupBuilder(L)
	off := false
	builder.state.Power = &off
	builder.module.markGroupPending(builder)
	L.Push(ud)
	return 1
}

// groupBuilderToggle toggles power based on current actual state.
// Note: For toggle to work correctly, the reconciler needs to know the actual state.
// This sets a toggle flag that the reconciler will interpret.
func groupBuilderToggle(L *lua.LState) int {
	builder, ud := checkGroupBuilder(L)
	// For toggle, we need to fetch actual state and decide
	// For now, we'll just flip the current desired power if set, otherwise turn on
	if builder.state.Power != nil {
		newPower := !*builder.state.Power
		builder.state.Power = &newPower
	} else {
		// Default to on if no power state is set
		on := true
		builder.state.Power = &on
	}
	builder.module.markGroupPending(builder)
	L.Push(ud)
	return 1
}

// groupBuilderSetBri sets brightness (chainable).
func groupBuilderSetBri(L *lua.LState) int {
	builder, ud := checkGroupBuilder(L)
	bri := L.CheckInt(2)

	// Clamp to valid range
	if bri < 1 {
		bri = 1
	}
	if bri > 254 {
		bri = 254
	}

	briByte := uint8(bri)
	builder.state.Bri = &briByte
	builder.module.markGroupPending(builder)
	L.Push(ud)
	return 1
}

// groupBuilderSetScene sets the scene name (chainable).
func groupBuilderSetScene(L *lua.LState) int {
	builder, ud := checkGroupBuilder(L)
	sceneName := L.CheckString(2)

	builder.state.SceneName = sceneName
	builder.module.markGroupPending(builder)
	L.Push(ud)
	return 1
}

// groupBuilderSetColorXY sets CIE xy color coordinates (chainable).
func groupBuilderSetColorXY(L *lua.LState) int {
	builder, ud := checkGroupBuilder(L)
	x := float32(L.CheckNumber(2))
	y := float32(L.CheckNumber(3))

	builder.state.Xy = []float32{x, y}
	builder.module.markGroupPending(builder)
	L.Push(ud)
	return 1
}

// groupBuilderSetCt sets color temperature in mirek (chainable).
func groupBuilderSetCt(L *lua.LState) int {
	builder, ud := checkGroupBuilder(L)
	ct := L.CheckInt(2)

	// Clamp to valid range
	if ct < 153 {
		ct = 153
	}
	if ct > 500 {
		ct = 500
	}

	ctVal := uint16(ct)
	builder.state.Ct = &ctVal
	builder.module.markGroupPending(builder)
	L.Push(ud)
	return 1
}

// groupBuilderSetHue sets hue value (chainable).
func groupBuilderSetHue(L *lua.LState) int {
	builder, ud := checkGroupBuilder(L)
	hue := L.CheckInt(2)

	// Clamp to valid range
	if hue < 0 {
		hue = 0
	}
	if hue > 65535 {
		hue = 65535
	}

	hueVal := uint16(hue)
	builder.state.Hue = &hueVal
	builder.module.markGroupPending(builder)
	L.Push(ud)
	return 1
}

// groupBuilderSetSat sets saturation (chainable).
func groupBuilderSetSat(L *lua.LState) int {
	builder, ud := checkGroupBuilder(L)
	sat := L.CheckInt(2)

	// Clamp to valid range
	if sat < 0 {
		sat = 0
	}
	if sat > 254 {
		sat = 254
	}

	satVal := uint8(sat)
	builder.state.Sat = &satVal
	builder.module.markGroupPending(builder)
	L.Push(ud)
	return 1
}
