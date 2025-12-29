package modules

import (
	"github.com/amimof/huego"
	"github.com/rs/zerolog/log"
	lua "github.com/yuin/gopher-lua"
)

const lightTypeName = "hue.light"

// LightUserdata wraps a huego.Light for Lua access
type LightUserdata struct {
	light *huego.Light
}

// RegisterLightType registers the hue.light metatable
func RegisterLightType(L *lua.LState) {
	mt := L.NewTypeMetatable(lightTypeName)
	L.SetField(mt, "__index", L.SetFuncs(L.NewTable(), lightMethods))
}

var lightMethods = map[string]lua.LGFunction{
	// Getters (return values)
	"id":      lightGetID,
	"name":    lightGetName,
	"is_on":   lightIsOn,
	"get_bri": lightGetBri,

	// Chainable setters (return self for chaining)
	"on":        lightOn,
	"off":       lightOff,
	"toggle":    lightToggle,
	"set_bri":   lightSetBri,
	"set_color": lightSetColorXY,
	"set_ct":    lightSetColorTemp,
	"set_hue":   lightSetHue,
	"set_sat":   lightSetSat,
	"alert":     lightAlert,

	// Generic state setter
	"set_state": lightSetState,
}

// pushLight creates a new Light userdata and pushes it onto the stack
func pushLight(L *lua.LState, light *huego.Light) {
	ud := L.NewUserData()
	ud.Value = &LightUserdata{light: light}
	L.SetMetatable(ud, L.GetTypeMetatable(lightTypeName))
	L.Push(ud)
}

// checkLight retrieves the LightUserdata from the Lua stack
func checkLight(L *lua.LState) (*LightUserdata, *lua.LUserData) {
	ud := L.CheckUserData(1)
	if v, ok := ud.Value.(*LightUserdata); ok {
		return v, ud
	}
	L.ArgError(1, "hue.light expected")
	return nil, nil
}

// =============================================================================
// Getters (return values, not chainable)
// =============================================================================

// lightGetID returns the light ID
// light:id() -> number
func lightGetID(L *lua.LState) int {
	light, _ := checkLight(L)
	L.Push(lua.LNumber(light.light.ID))
	return 1
}

// lightGetName returns the light name
// light:name() -> string
func lightGetName(L *lua.LState) int {
	light, _ := checkLight(L)
	L.Push(lua.LString(light.light.Name))
	return 1
}

// lightIsOn returns whether the light is on
// light:is_on() -> bool
func lightIsOn(L *lua.LState) int {
	light, _ := checkLight(L)
	L.Push(lua.LBool(light.light.State.On))
	return 1
}

// lightGetBri gets the current brightness
// light:get_bri() -> number
func lightGetBri(L *lua.LState) int {
	light, _ := checkLight(L)
	L.Push(lua.LNumber(light.light.State.Bri))
	return 1
}

// =============================================================================
// Chainable Setters (return self for chaining)
// =============================================================================

// lightOn turns the light on (chainable)
// light:on() -> self
func lightOn(L *lua.LState) int {
	light, ud := checkLight(L)
	err := light.light.On()
	if err != nil {
		log.Error().Err(err).Int("light", light.light.ID).Msg("Failed to turn on light")
	}
	L.Push(ud)
	return 1
}

// lightOff turns the light off (chainable)
// light:off() -> self
func lightOff(L *lua.LState) int {
	light, ud := checkLight(L)
	err := light.light.Off()
	if err != nil {
		log.Error().Err(err).Int("light", light.light.ID).Msg("Failed to turn off light")
	}
	L.Push(ud)
	return 1
}

// lightToggle toggles the light on/off (chainable)
// light:toggle() -> self
func lightToggle(L *lua.LState) int {
	light, ud := checkLight(L)
	var err error
	if light.light.State.On {
		err = light.light.Off()
	} else {
		err = light.light.On()
	}
	if err != nil {
		log.Error().Err(err).Int("light", light.light.ID).Msg("Failed to toggle light")
	}
	L.Push(ud)
	return 1
}

// lightSetBri sets the light brightness (1-254) (chainable)
// light:set_bri(value) -> self
func lightSetBri(L *lua.LState) int {
	light, ud := checkLight(L)
	bri := L.CheckInt(2)

	// Clamp to valid range
	if bri < 1 {
		bri = 1
	}
	if bri > 254 {
		bri = 254
	}

	err := light.light.Bri(uint8(bri))
	if err != nil {
		log.Error().Err(err).Int("light", light.light.ID).Int("bri", bri).Msg("Failed to set brightness")
	}
	L.Push(ud)
	return 1
}

// lightSetColorXY sets the light color using CIE xy coordinates (chainable)
// light:set_color(x, y) -> self
func lightSetColorXY(L *lua.LState) int {
	light, ud := checkLight(L)
	x := float32(L.CheckNumber(2))
	y := float32(L.CheckNumber(3))

	err := light.light.Xy([]float32{x, y})
	if err != nil {
		log.Error().Err(err).Int("light", light.light.ID).Msg("Failed to set color XY")
	}
	L.Push(ud)
	return 1
}

// lightSetColorTemp sets the color temperature in mirek (153-500) (chainable)
// light:set_ct(mirek) -> self
func lightSetColorTemp(L *lua.LState) int {
	light, ud := checkLight(L)
	mirek := L.CheckInt(2)

	// Clamp to valid range
	if mirek < 153 {
		mirek = 153
	}
	if mirek > 500 {
		mirek = 500
	}

	err := light.light.Ct(uint16(mirek))
	if err != nil {
		log.Error().Err(err).Int("light", light.light.ID).Int("mirek", mirek).Msg("Failed to set color temp")
	}
	L.Push(ud)
	return 1
}

// lightSetHue sets the hue value (0-65535) (chainable)
// light:set_hue(value) -> self
func lightSetHue(L *lua.LState) int {
	light, ud := checkLight(L)
	hue := L.CheckInt(2)

	// Clamp to valid range
	if hue < 0 {
		hue = 0
	}
	if hue > 65535 {
		hue = 65535
	}

	err := light.light.Hue(uint16(hue))
	if err != nil {
		log.Error().Err(err).Int("light", light.light.ID).Int("hue", hue).Msg("Failed to set hue")
	}
	L.Push(ud)
	return 1
}

// lightSetSat sets the saturation (0-254) (chainable)
// light:set_sat(value) -> self
func lightSetSat(L *lua.LState) int {
	light, ud := checkLight(L)
	sat := L.CheckInt(2)

	// Clamp to valid range
	if sat < 0 {
		sat = 0
	}
	if sat > 254 {
		sat = 254
	}

	err := light.light.Sat(uint8(sat))
	if err != nil {
		log.Error().Err(err).Int("light", light.light.ID).Int("sat", sat).Msg("Failed to set saturation")
	}
	L.Push(ud)
	return 1
}

// lightAlert triggers an alert on the light (chainable)
// light:alert(type) -> self
// type: "none", "select" (single flash), "lselect" (15 second flash)
func lightAlert(L *lua.LState) int {
	light, ud := checkLight(L)
	alertType := L.OptString(2, "select")

	err := light.light.Alert(alertType)
	if err != nil {
		log.Error().Err(err).Int("light", light.light.ID).Str("alert", alertType).Msg("Failed to set alert")
	}
	L.Push(ud)
	return 1
}

// lightSetState sets multiple state properties at once (chainable)
// light:set_state({on = true, bri = 200, hue = 40000, sat = 254, xy = {0.5, 0.4}, ct = 300}) -> self
func lightSetState(L *lua.LState) int {
	light, ud := checkLight(L)
	tbl := L.CheckTable(2)

	state := huego.State{}
	hasState := false

	// on
	if v := tbl.RawGetString("on"); v != lua.LNil {
		if on, ok := v.(lua.LBool); ok {
			state.On = bool(on)
			hasState = true
		}
	}

	// bri
	if v := tbl.RawGetString("bri"); v != lua.LNil {
		if bri, ok := v.(lua.LNumber); ok {
			b := int(bri)
			if b < 1 {
				b = 1
			}
			if b > 254 {
				b = 254
			}
			state.Bri = uint8(b)
			hasState = true
		}
	}

	// hue
	if v := tbl.RawGetString("hue"); v != lua.LNil {
		if hue, ok := v.(lua.LNumber); ok {
			h := int(hue)
			if h < 0 {
				h = 0
			}
			if h > 65535 {
				h = 65535
			}
			state.Hue = uint16(h)
			hasState = true
		}
	}

	// sat
	if v := tbl.RawGetString("sat"); v != lua.LNil {
		if sat, ok := v.(lua.LNumber); ok {
			s := int(sat)
			if s < 0 {
				s = 0
			}
			if s > 254 {
				s = 254
			}
			state.Sat = uint8(s)
			hasState = true
		}
	}

	// ct (color temperature)
	if v := tbl.RawGetString("ct"); v != lua.LNil {
		if ct, ok := v.(lua.LNumber); ok {
			c := int(ct)
			if c < 153 {
				c = 153
			}
			if c > 500 {
				c = 500
			}
			state.Ct = uint16(c)
			hasState = true
		}
	}

	// xy (color coordinates)
	if v := tbl.RawGetString("xy"); v != lua.LNil {
		if xyTbl, ok := v.(*lua.LTable); ok {
			x := float32(0)
			y := float32(0)
			if xv := xyTbl.RawGetInt(1); xv != lua.LNil {
				if xn, ok := xv.(lua.LNumber); ok {
					x = float32(xn)
				}
			}
			if yv := xyTbl.RawGetInt(2); yv != lua.LNil {
				if yn, ok := yv.(lua.LNumber); ok {
					y = float32(yn)
				}
			}
			state.Xy = []float32{x, y}
			hasState = true
		}
	}

	// transitiontime (optional)
	if v := tbl.RawGetString("transitiontime"); v != lua.LNil {
		if tt, ok := v.(lua.LNumber); ok {
			t := uint16(tt)
			state.TransitionTime = t
			hasState = true
		}
	}

	// alert
	if v := tbl.RawGetString("alert"); v != lua.LNil {
		if alert, ok := v.(lua.LString); ok {
			state.Alert = string(alert)
			hasState = true
		}
	}

	// effect
	if v := tbl.RawGetString("effect"); v != lua.LNil {
		if effect, ok := v.(lua.LString); ok {
			state.Effect = string(effect)
			hasState = true
		}
	}

	if hasState {
		err := light.light.SetState(state)
		if err != nil {
			log.Error().Err(err).Int("light", light.light.ID).Msg("Failed to set state")
		}
	}

	L.Push(ud)
	return 1
}
