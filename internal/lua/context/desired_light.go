package context

import (
	lua "github.com/yuin/gopher-lua"

	"github.com/dokzlo13/lightd/internal/hue/reconcile/light"
)

const lightBuilderTypeName = "desired.light"

// LightDesiredBuilder accumulates desired state changes for a light.
// Used by ctx.desired:light(id) to provide chainable methods.
type LightDesiredBuilder struct {
	lightID string
	state   light.Desired
	module  *DesiredModule // reference back for pending tracking
}

// RegisterLightBuilderType registers the desired.light metatable.
func RegisterLightBuilderType(L *lua.LState) {
	mt := L.NewTypeMetatable(lightBuilderTypeName)
	L.SetField(mt, "__index", L.SetFuncs(L.NewTable(), lightBuilderMethods))
}

var lightBuilderMethods = map[string]lua.LGFunction{
	"on":        lightBuilderOn,
	"off":       lightBuilderOff,
	"toggle":    lightBuilderToggle,
	"set_bri":   lightBuilderSetBri,
	"set_color": lightBuilderSetColorXY,
	"set_ct":    lightBuilderSetCt,
	"set_hue":   lightBuilderSetHue,
	"set_sat":   lightBuilderSetSat,
}

// pushLightBuilder creates a new LightDesiredBuilder userdata and pushes it onto the stack.
func pushLightBuilder(L *lua.LState, lightID string, module *DesiredModule) {
	ud := L.NewUserData()
	ud.Value = &LightDesiredBuilder{
		lightID: lightID,
		state:   light.Desired{},
		module:  module,
	}
	L.SetMetatable(ud, L.GetTypeMetatable(lightBuilderTypeName))
	L.Push(ud)
}

// checkLightBuilder retrieves the LightDesiredBuilder from the Lua stack.
func checkLightBuilder(L *lua.LState) (*LightDesiredBuilder, *lua.LUserData) {
	ud := L.CheckUserData(1)
	if v, ok := ud.Value.(*LightDesiredBuilder); ok {
		return v, ud
	}
	L.ArgError(1, "desired.light expected")
	return nil, nil
}

// lightBuilderOn sets power to on (chainable).
func lightBuilderOn(L *lua.LState) int {
	builder, ud := checkLightBuilder(L)
	on := true
	builder.state.Power = &on
	builder.module.markLightPending(builder)
	L.Push(ud)
	return 1
}

// lightBuilderOff sets power to off (chainable).
func lightBuilderOff(L *lua.LState) int {
	builder, ud := checkLightBuilder(L)
	off := false
	builder.state.Power = &off
	builder.module.markLightPending(builder)
	L.Push(ud)
	return 1
}

// lightBuilderToggle toggles power based on current state.
func lightBuilderToggle(L *lua.LState) int {
	builder, ud := checkLightBuilder(L)
	// For toggle, flip the current desired power if set, otherwise turn on
	if builder.state.Power != nil {
		newPower := !*builder.state.Power
		builder.state.Power = &newPower
	} else {
		on := true
		builder.state.Power = &on
	}
	builder.module.markLightPending(builder)
	L.Push(ud)
	return 1
}

// lightBuilderSetBri sets brightness (chainable).
func lightBuilderSetBri(L *lua.LState) int {
	builder, ud := checkLightBuilder(L)
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
	builder.module.markLightPending(builder)
	L.Push(ud)
	return 1
}

// lightBuilderSetColorXY sets CIE xy color coordinates (chainable).
func lightBuilderSetColorXY(L *lua.LState) int {
	builder, ud := checkLightBuilder(L)
	x := float32(L.CheckNumber(2))
	y := float32(L.CheckNumber(3))

	builder.state.Xy = []float32{x, y}
	builder.module.markLightPending(builder)
	L.Push(ud)
	return 1
}

// lightBuilderSetCt sets color temperature in mirek (chainable).
func lightBuilderSetCt(L *lua.LState) int {
	builder, ud := checkLightBuilder(L)
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
	builder.module.markLightPending(builder)
	L.Push(ud)
	return 1
}

// lightBuilderSetHue sets hue value (chainable).
func lightBuilderSetHue(L *lua.LState) int {
	builder, ud := checkLightBuilder(L)
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
	builder.module.markLightPending(builder)
	L.Push(ud)
	return 1
}

// lightBuilderSetSat sets saturation (chainable).
func lightBuilderSetSat(L *lua.LState) int {
	builder, ud := checkLightBuilder(L)
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
	builder.module.markLightPending(builder)
	L.Push(ud)
	return 1
}
