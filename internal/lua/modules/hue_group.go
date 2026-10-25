package modules

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/dokzlo13/lightd/internal/hue/bridge"
	lua "github.com/yuin/gopher-lua"
)

var nativeHueMethods = map[string]lua.LGFunction{
	"id":   func(L *lua.LState) int { r, _ := checkNativeHue(L); L.Push(lua.LString(r.id)); return 1 },
	"name": hueName, "get_state": hueGetState, "get_brightness": hueBrightness,
	"is_on": hueAnyOn, "any_on": hueAnyOn, "all_on": hueAllOn, "members": hueMembers,
	"on": func(L *lua.LState) int {
		r, _ := checkNativeHue(L)
		v := true
		return hueApply(L, r, bridge.State{On: &v, PowerMode: "enforce"})
	},
	"off": func(L *lua.LState) int {
		r, _ := checkNativeHue(L)
		v := false
		return hueApply(L, r, bridge.State{On: &v, PowerMode: "enforce"})
	},
	"toggle": hueToggle, "set_brightness": hueSetBrightness, "set_ct": hueSetCT,
	"set_color": hueSetXY, "set_hsv": hueSetHSV, "set_scene": hueSetScene,
	"set_state": hueSetState, "effect": hueEffect, "signal": hueSignal,
	"snapshot": hueSnapshot, "restore": hueRestore,
}

func hueName(L *lua.LState) int {
	r, _ := checkNativeHue(L)
	var name string
	if r.kind == "group" {
		g, err := r.client.Group(hueContext(L), r.id)
		if err != nil {
			L.RaiseError("%s", err)
			return 0
		}
		name = g.Name
	} else {
		l, err := r.client.Light(hueContext(L), r.id)
		if err != nil {
			L.RaiseError("%s", err)
			return 0
		}
		name = l.Name
	}
	L.Push(lua.LString(name))
	return 1
}
func hueGetState(L *lua.LState) int {
	r, _ := checkNativeHue(L)
	if r.kind == "group" {
		g, err := r.client.Group(hueContext(L), r.id)
		if err != nil {
			L.RaiseError("%s", err)
			return 0
		}
		pushHueJSON(L, g)
	} else {
		l, err := r.client.Light(hueContext(L), r.id)
		if err != nil {
			L.RaiseError("%s", err)
			return 0
		}
		pushHueJSON(L, l)
	}
	return 1
}
func hueBrightness(L *lua.LState) int {
	r, _ := checkNativeHue(L)
	var v *float64
	if r.kind == "group" {
		g, err := r.client.Group(hueContext(L), r.id)
		if err != nil {
			L.RaiseError("%s", err)
			return 0
		}
		v = g.State.Brightness
	} else {
		l, err := r.client.Light(hueContext(L), r.id)
		if err != nil {
			L.RaiseError("%s", err)
			return 0
		}
		v = l.State.Brightness
	}
	if v == nil {
		L.Push(lua.LNil)
	} else {
		L.Push(lua.LNumber(*v))
	}
	return 1
}
func nativeHuePower(L *lua.LState, r *nativeHueResource) (bool, bool, error) {
	if r.kind == "group" {
		g, err := r.client.Group(hueContext(L), r.id)
		return g.AnyOn, g.AllOn, err
	}
	l, err := r.client.Light(hueContext(L), r.id)
	on := l.State.On != nil && *l.State.On
	return on, on, err
}
func hueAnyOn(L *lua.LState) int {
	r, _ := checkNativeHue(L)
	anyOn, _, err := nativeHuePower(L, r)
	if err != nil {
		L.RaiseError("%s", err)
		return 0
	}
	L.Push(lua.LBool(anyOn))
	return 1
}
func hueAllOn(L *lua.LState) int {
	r, _ := checkNativeHue(L)
	_, allOn, err := nativeHuePower(L, r)
	if err != nil {
		L.RaiseError("%s", err)
		return 0
	}
	L.Push(lua.LBool(allOn))
	return 1
}
func hueMembers(L *lua.LState) int {
	r, _ := checkNativeHue(L)
	if r.kind != "group" {
		L.RaiseError("members requires a group")
		return 0
	}
	g, err := r.client.Group(hueContext(L), r.id)
	if err != nil {
		L.RaiseError("%s", err)
		return 0
	}
	pushHueJSON(L, g.Members)
	return 1
}
func hueToggle(L *lua.LState) int {
	r, _ := checkNativeHue(L)
	on, _, err := nativeHuePower(L, r)
	if err != nil {
		L.RaiseError("%s", err)
		return 0
	}
	on = !on
	return hueApply(L, r, bridge.State{On: &on, PowerMode: "enforce"})
}
func hueSetBrightness(L *lua.LState) int {
	r, _ := checkNativeHue(L)
	v := float64(L.CheckNumber(2))
	return hueApply(L, r, bridge.State{Brightness: &v})
}
func hueSetCT(L *lua.LState) int {
	r, _ := checkNativeHue(L)
	n := L.CheckInt(2)
	if n < 1 || n > 65535 {
		L.ArgError(2, "positive mirek required")
		return 0
	}
	v := uint16(n)
	return hueApply(L, r, bridge.State{Ct: &v})
}
func hueSetXY(L *lua.LState) int {
	r, _ := checkNativeHue(L)
	return hueApply(L, r, bridge.State{Xy: []float32{float32(L.CheckNumber(2)), float32(L.CheckNumber(3))}})
}
func hueSetHSV(L *lua.LState) int {
	r, _ := checkNativeHue(L)
	xy, err := bridge.HSVToXY(float64(L.CheckNumber(2)), float64(L.CheckNumber(3)))
	if err != nil {
		L.RaiseError("%s", err)
		return 0
	}
	return hueApply(L, r, bridge.State{Xy: []float32{float32(xy[0]), float32(xy[1])}})
}
func hueSetScene(L *lua.LState) int {
	r, _ := checkNativeHue(L)
	if r.kind != "group" {
		L.RaiseError("set_scene requires a group")
		return 0
	}
	s := bridge.State{}
	if L.OptBool(3, false) {
		on := true
		s.On = &on
		s.PowerMode = "enforce"
	}
	if err := r.client.ApplyGroup(hueContext(L), r.id, s, L.CheckString(2)); err != nil {
		L.RaiseError("%s", err)
		return 0
	}
	L.Push(L.Get(1))
	return 1
}
func hueSetState(L *lua.LState) int {
	r, _ := checkNativeHue(L)
	data, err := json.Marshal(LuaToGo(L.CheckTable(2)))
	if err != nil {
		L.RaiseError("%s", err)
		return 0
	}
	var s bridge.State
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err = d.Decode(&s); err != nil {
		L.RaiseError("invalid native Hue state: %s", err)
		return 0
	}
	return hueApply(L, r, s)
}
func hueEffect(L *lua.LState) int {
	r, _ := checkNativeHue(L)
	return hueApply(L, r, bridge.State{Effect: L.CheckString(2)})
}
func hueSignal(L *lua.LState) int {
	r, _ := checkNativeHue(L)
	if err := r.client.Signal(hueContext(L), r.kind, r.id, L.CheckString(2), L.CheckInt(3)); err != nil {
		L.RaiseError("%s", err)
		return 0
	}
	L.Push(L.Get(1))
	return 1
}
func hueSnapshot(L *lua.LState) int {
	r, _ := checkNativeHue(L)
	s, err := r.client.Snapshot(hueContext(L), r.kind, r.id)
	if err != nil {
		L.RaiseError("%s", err)
		return 0
	}
	pushHueJSON(L, s)
	return 1
}
func hueRestore(L *lua.LState) int {
	r, _ := checkNativeHue(L)
	var s bridge.Snapshot
	if err := hueInput(L, 2, &s); err != nil {
		L.RaiseError("%s", err)
		return 0
	}
	if len(s.Lights) == 0 {
		L.RaiseError("%s", fmt.Errorf("snapshot contains no lights"))
		return 0
	}
	if err := r.client.Restore(hueContext(L), s, L.OptBool(3, false)); err != nil {
		L.RaiseError("%s", err)
		return 0
	}
	L.Push(L.Get(1))
	return 1
}
