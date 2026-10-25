package context

import (
	"github.com/dokzlo13/lightd/internal/hue/bridge"
	lua "github.com/yuin/gopher-lua"
	"math"

	"github.com/dokzlo13/lightd/internal/hue/reconcile/light"
)

const lightBuilderTypeName = "desired.light"

// LightDesiredBuilder accumulates desired state changes for a light.
// Used by ctx.desired:light(id) to provide chainable methods.
type LightDesiredBuilder struct {
	lightID string
	state   light.Desired
	extra   map[string]any
	clear   []string
	module  *DesiredModule // reference back for pending tracking
}

// RegisterLightBuilderType registers the desired.light metatable.
func RegisterLightBuilderType(L *lua.LState) {
	mt := L.NewTypeMetatable(lightBuilderTypeName)
	L.SetField(mt, "__index", L.SetFuncs(L.NewTable(), lightBuilderMethods))
}

var lightBuilderMethods = map[string]lua.LGFunction{
	"on":             lightBuilderOn,
	"off":            lightBuilderOff,
	"toggle":         lightBuilderToggle,
	"set_hsv":        lightBuilderSetHSV,
	"set_brightness": lightBuilderSetBrightness,
	"set_transition": lightBuilderSetTransition,
	"preserve_power": lightBuilderPreservePower,
	"next_on":        lightBuilderNextOn,
	"clear_scene":    lightBuilderClearScene,

	"set_color": lightBuilderSetColorXY,
	"set_ct":    lightBuilderSetCt,
}

// pushLightBuilder creates a new LightDesiredBuilder userdata and pushes it onto the stack.
func pushLightBuilder(L *lua.LState, lightID string, module *DesiredModule) {
	if module.closed {
		L.RaiseError("desired builder belongs to a finished action")
		return
	}
	ud := L.NewUserData()
	if existing, ok := module.lightBuilders[lightID]; ok {
		ud.Value = existing
	} else {
		ud.Value = &LightDesiredBuilder{
			lightID: lightID,
			state:   light.Desired{},
			extra:   map[string]any{},
			module:  module,
		}
	}
	module.lightBuilders[lightID] = ud.Value.(*LightDesiredBuilder)
	L.SetMetatable(ud, L.GetTypeMetatable(lightBuilderTypeName))
	L.Push(ud)
}

// checkLightBuilder retrieves the LightDesiredBuilder from the Lua stack.
func checkLightBuilder(L *lua.LState) (*LightDesiredBuilder, *lua.LUserData) {
	ud := L.CheckUserData(1)
	if v, ok := ud.Value.(*LightDesiredBuilder); ok {
		if v.module.closed {
			L.RaiseError("desired builder belongs to a finished action")
			return nil, nil
		}
		return v, ud
	}
	L.ArgError(1, "desired.light expected")
	return nil, nil
}

// lightBuilderOn sets power to on (chainable).
func lightBuilderOn(L *lua.LState) int {
	builder, ud := checkLightBuilder(L)
	builder.extra["power_mode"] = "enforce"
	on := true
	builder.state.On = &on
	builder.module.markLightPending(builder)
	L.Push(ud)
	return 1
}

// lightBuilderOff sets power to off (chainable).
func lightBuilderOff(L *lua.LState) int {
	builder, ud := checkLightBuilder(L)
	builder.extra["power_mode"] = "enforce"
	off := false
	builder.state.On = &off
	builder.module.markLightPending(builder)
	L.Push(ud)
	return 1
}

// lightBuilderToggle toggles power based on current state.
func lightBuilderToggle(L *lua.LState) int {
	builder, ud := checkLightBuilder(L)
	if builder.state.On == nil {
		actual, err := builder.module.actualLight.Get(L.Context(), builder.lightID)
		if err != nil {
			L.RaiseError("read power for toggle: %s", err)
			return 0
		}
		on := !(actual.On != nil && *actual.On)
		builder.state.On = &on
	} else {
		on := !*builder.state.On
		builder.state.On = &on
	}
	builder.extra["power_mode"] = "enforce"
	builder.module.markLightPending(builder)
	L.Push(ud)
	return 1

}

// lightBuilderSetBri sets brightness (chainable).
func lightBuilderSetBri(L *lua.LState) int {
	builder, ud := checkLightBuilder(L)
	builder.clear = append(builder.clear, "scene_name")
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
	builder.clear = append(builder.clear, "scene_name")
	builder.state.Ct = nil
	builder.state.Hue = nil
	builder.state.Sat = nil
	builder.clear = append(builder.clear, "ct", "hue", "sat")
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
	builder.clear = append(builder.clear, "scene_name")
	builder.state.Xy = nil
	builder.state.Hue = nil
	builder.state.Sat = nil
	builder.clear = append(builder.clear, "xy", "hue", "sat")
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
	builder.clear = append(builder.clear, "scene_name")
	builder.state.Ct = nil
	builder.state.Xy = nil
	builder.clear = append(builder.clear, "ct", "xy")
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
	builder.clear = append(builder.clear, "scene_name")
	builder.state.Ct = nil
	builder.state.Xy = nil
	builder.clear = append(builder.clear, "ct", "xy")
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

func lightBuilderSetBrightness(L *lua.LState) int {
	b, ud := checkLightBuilder(L)
	v := float64(L.CheckNumber(2))
	if v < 0 || v > 100 || math.IsNaN(v) || math.IsInf(v, 0) {
		L.ArgError(2, "brightness must be finite 0..100 percent")
		return 0
	}
	b.extra["brightness"] = v
	b.state.Bri = nil
	b.clear = append(b.clear, "bri", "scene_name")
	b.module.markLightPending(b)
	L.Push(ud)
	return 1
}
func lightBuilderSetTransition(L *lua.LState) int {
	b, ud := checkLightBuilder(L)
	v := L.CheckInt(2)
	if v < 0 || v > 3600000 {
		L.ArgError(2, "transition must be 0..3600000 ms")
		return 0
	}
	b.extra["duration"] = v
	b.state.TransitionTime = nil
	b.clear = append(b.clear, "transitiontime")
	b.module.markLightPending(b)
	L.Push(ud)
	return 1
}
func lightBuilderPreservePower(L *lua.LState) int {
	b, ud := checkLightBuilder(L)
	b.state.On = nil
	b.clear = append(b.clear, "on")
	b.extra["power_mode"] = "preserve"
	b.module.markLightPending(b)
	L.Push(ud)
	return 1
}
func lightBuilderNextOn(L *lua.LState) int {
	b, ud := checkLightBuilder(L)
	b.state.On = nil
	b.clear = append(b.clear, "on")
	b.extra["power_mode"] = "next_on"
	b.module.markLightPending(b)
	L.Push(ud)
	return 1
}
func lightBuilderClearScene(L *lua.LState) int {
	b, ud := checkLightBuilder(L)
	b.clear = append(b.clear, "scene_name")
	b.module.markLightPending(b)
	L.Push(ud)
	return 1
}

func lightBuilderSetHSV(L *lua.LState) int {
	b, ud := checkLightBuilder(L)
	xy, err := bridge.HSVToXY(float64(L.CheckNumber(2)), float64(L.CheckNumber(3)))
	if err != nil {
		L.RaiseError("HSV: %s", err)
		return 0
	}
	b.state.Ct = nil
	b.state.Hue = nil
	b.state.Sat = nil
	b.state.Xy = []float32{float32(xy[0]), float32(xy[1])}
	b.clear = append(b.clear, "scene_name", "ct", "hue", "sat")
	b.module.markLightPending(b)
	L.Push(ud)
	return 1
}
