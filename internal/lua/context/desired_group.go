package context

import (
	"github.com/dokzlo13/lightd/internal/hue/bridge"
	lua "github.com/yuin/gopher-lua"
	"math"

	"github.com/dokzlo13/lightd/internal/hue/reconcile/group"
)

const groupBuilderTypeName = "desired.group"

// GroupDesiredBuilder accumulates desired state changes for a group.
// Used by ctx.desired:group(id) to provide chainable methods.
type GroupDesiredBuilder struct {
	groupID string
	state   group.Desired
	extra   map[string]any
	clear   []string
	module  *DesiredModule // reference back for pending tracking
}

// RegisterGroupBuilderType registers the desired.group metatable.
func RegisterGroupBuilderType(L *lua.LState) {
	mt := L.NewTypeMetatable(groupBuilderTypeName)
	L.SetField(mt, "__index", L.SetFuncs(L.NewTable(), groupBuilderMethods))
}

var groupBuilderMethods = map[string]lua.LGFunction{
	"on":             groupBuilderOn,
	"off":            groupBuilderOff,
	"toggle":         groupBuilderToggle,
	"set_hsv":        groupBuilderSetHSV,
	"set_brightness": groupBuilderSetBrightness,
	"set_transition": groupBuilderSetTransition,
	"preserve_power": groupBuilderPreservePower,
	"next_on":        groupBuilderNextOn,
	"clear_scene":    groupBuilderClearScene,

	"set_scene": groupBuilderSetScene,
	"set_color": groupBuilderSetColorXY,
	"set_ct":    groupBuilderSetCt,
}

// pushGroupBuilder creates a new GroupDesiredBuilder userdata and pushes it onto the stack.
func pushGroupBuilder(L *lua.LState, groupID string, module *DesiredModule) {
	if module.closed {
		L.RaiseError("desired builder belongs to a finished action")
		return
	}
	ud := L.NewUserData()
	if existing, ok := module.groupBuilders[groupID]; ok {
		ud.Value = existing
	} else {
		ud.Value = &GroupDesiredBuilder{
			groupID: groupID,
			state:   group.Desired{},
			extra:   map[string]any{},
			module:  module,
		}
	}
	module.groupBuilders[groupID] = ud.Value.(*GroupDesiredBuilder)
	L.SetMetatable(ud, L.GetTypeMetatable(groupBuilderTypeName))
	L.Push(ud)
}

// checkGroupBuilder retrieves the GroupDesiredBuilder from the Lua stack.
func checkGroupBuilder(L *lua.LState) (*GroupDesiredBuilder, *lua.LUserData) {
	ud := L.CheckUserData(1)
	if v, ok := ud.Value.(*GroupDesiredBuilder); ok {
		if v.module.closed {
			L.RaiseError("desired builder belongs to a finished action")
			return nil, nil
		}
		return v, ud
	}
	L.ArgError(1, "desired.group expected")
	return nil, nil
}

// groupBuilderOn sets power to on (chainable).
func groupBuilderOn(L *lua.LState) int {
	builder, ud := checkGroupBuilder(L)
	builder.extra["power_mode"] = "enforce"
	on := true
	builder.state.On = &on
	builder.module.markGroupPending(builder)
	L.Push(ud)
	return 1
}

// groupBuilderOff sets power to off (chainable).
func groupBuilderOff(L *lua.LState) int {
	builder, ud := checkGroupBuilder(L)
	builder.extra["power_mode"] = "enforce"
	off := false
	builder.state.On = &off
	builder.module.markGroupPending(builder)
	L.Push(ud)
	return 1
}

// groupBuilderToggle toggles power based on current actual state.
// Note: For toggle to work correctly, the reconciler needs to know the actual state.
// This sets a toggle flag that the reconciler will interpret.
func groupBuilderToggle(L *lua.LState) int {
	builder, ud := checkGroupBuilder(L)
	if builder.state.On == nil {
		actual, err := builder.module.actualGroup.Get(L.Context(), builder.groupID)
		if err != nil {
			L.RaiseError("read power for toggle: %s", err)
			return 0
		}
		on := !actual.AnyOn
		builder.state.On = &on
	} else {
		on := !*builder.state.On
		builder.state.On = &on
	}
	builder.extra["power_mode"] = "enforce"
	builder.module.markGroupPending(builder)
	L.Push(ud)
	return 1

}

// groupBuilderSetBri sets brightness (chainable).
func groupBuilderSetBri(L *lua.LState) int {
	builder, ud := checkGroupBuilder(L)
	builder.clear = append(builder.clear, "scene_name")
	builder.state.SceneName = ""
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

	builder.state.Bri = nil
	builder.state.Ct = nil
	builder.state.Xy = nil
	builder.state.Hue = nil
	builder.state.Sat = nil
	delete(builder.extra, "brightness")
	builder.clear = append(builder.clear, "scene_name", "bri", "brightness", "ct", "xy", "hue", "sat")
	builder.state.SceneName = sceneName
	builder.module.markGroupPending(builder)
	L.Push(ud)
	return 1
}

// groupBuilderSetColorXY sets CIE xy color coordinates (chainable).
func groupBuilderSetColorXY(L *lua.LState) int {
	builder, ud := checkGroupBuilder(L)
	builder.clear = append(builder.clear, "scene_name")
	builder.state.SceneName = ""
	builder.state.Ct = nil
	builder.state.Hue = nil
	builder.state.Sat = nil
	builder.clear = append(builder.clear, "ct", "hue", "sat")
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
	builder.clear = append(builder.clear, "scene_name")
	builder.state.SceneName = ""
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
	builder.module.markGroupPending(builder)
	L.Push(ud)
	return 1
}

// groupBuilderSetHue sets hue value (chainable).
func groupBuilderSetHue(L *lua.LState) int {
	builder, ud := checkGroupBuilder(L)
	builder.clear = append(builder.clear, "scene_name")
	builder.state.SceneName = ""
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
	builder.module.markGroupPending(builder)
	L.Push(ud)
	return 1
}

// groupBuilderSetSat sets saturation (chainable).
func groupBuilderSetSat(L *lua.LState) int {
	builder, ud := checkGroupBuilder(L)
	builder.clear = append(builder.clear, "scene_name")
	builder.state.SceneName = ""
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
	builder.module.markGroupPending(builder)
	L.Push(ud)
	return 1
}

func groupBuilderSetBrightness(L *lua.LState) int {
	b, ud := checkGroupBuilder(L)
	v := float64(L.CheckNumber(2))
	if v < 0 || v > 100 || math.IsNaN(v) || math.IsInf(v, 0) {
		L.ArgError(2, "brightness must be finite 0..100 percent")
		return 0
	}
	b.extra["brightness"] = v
	b.state.Bri = nil
	b.clear = append(b.clear, "bri", "scene_name")
	b.state.SceneName = ""
	b.module.markGroupPending(b)
	L.Push(ud)
	return 1
}
func groupBuilderSetTransition(L *lua.LState) int {
	b, ud := checkGroupBuilder(L)
	v := L.CheckInt(2)
	if v < 0 || v > 3600000 {
		L.ArgError(2, "transition must be 0..3600000 ms")
		return 0
	}
	b.extra["duration"] = v
	b.state.TransitionTime = nil
	b.clear = append(b.clear, "transitiontime")
	b.module.markGroupPending(b)
	L.Push(ud)
	return 1
}
func groupBuilderPreservePower(L *lua.LState) int {
	b, ud := checkGroupBuilder(L)
	b.state.On = nil
	b.clear = append(b.clear, "on")
	b.extra["power_mode"] = "preserve"
	b.module.markGroupPending(b)
	L.Push(ud)
	return 1
}
func groupBuilderNextOn(L *lua.LState) int {
	b, ud := checkGroupBuilder(L)
	b.state.On = nil
	b.clear = append(b.clear, "on")
	b.extra["power_mode"] = "next_on"
	b.module.markGroupPending(b)
	L.Push(ud)
	return 1
}
func groupBuilderClearScene(L *lua.LState) int {
	b, ud := checkGroupBuilder(L)
	b.clear = append(b.clear, "scene_name")
	b.state.SceneName = ""
	b.module.markGroupPending(b)
	L.Push(ud)
	return 1
}

func groupBuilderSetHSV(L *lua.LState) int {
	b, ud := checkGroupBuilder(L)
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
	b.state.SceneName = ""
	b.module.markGroupPending(b)
	L.Push(ud)
	return 1
}
