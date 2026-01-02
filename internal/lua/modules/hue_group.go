package modules

import (
	"strconv"

	"github.com/amimof/huego"
	"github.com/rs/zerolog/log"
	lua "github.com/yuin/gopher-lua"

	"github.com/dokzlo13/lightd/internal/hue"
)

const groupTypeName = "hue.group"

// GroupUserdata wraps a huego.Group for Lua access
type GroupUserdata struct {
	group      *huego.Group
	sceneIndex *hue.SceneIndex
}

// RegisterGroupType registers the hue.group metatable
func RegisterGroupType(L *lua.LState) {
	mt := L.NewTypeMetatable(groupTypeName)
	L.SetField(mt, "__index", L.SetFuncs(L.NewTable(), groupMethods))
}

var groupMethods = map[string]lua.LGFunction{
	// Getters (return values)
	"id":      groupGetID,
	"name":    groupGetName,
	"is_on":   groupIsOn,
	"all_on":  groupAllOn,
	"any_on":  groupAnyOn,
	"get_bri": groupGetBri,
	"lights":  groupGetLights,

	// Chainable setters (return self for chaining)
	"on":        groupOn,
	"off":       groupOff,
	"toggle":    groupToggle,
	"set_bri":   groupSetBri,
	"set_scene": groupSetScene,
	"set_color": groupSetColorXY,

	// Generic state setter
	"set_state": groupSetState,
}

// pushGroup creates a new Group userdata and pushes it onto the stack
func pushGroup(L *lua.LState, group *huego.Group, sceneIndex *hue.SceneIndex) {
	ud := L.NewUserData()
	ud.Value = &GroupUserdata{group: group, sceneIndex: sceneIndex}
	L.SetMetatable(ud, L.GetTypeMetatable(groupTypeName))
	L.Push(ud)
}

// checkGroup retrieves the GroupUserdata from the Lua stack
func checkGroup(L *lua.LState) (*GroupUserdata, *lua.LUserData) {
	ud := L.CheckUserData(1)
	if v, ok := ud.Value.(*GroupUserdata); ok {
		return v, ud
	}
	L.ArgError(1, "hue.group expected")
	return nil, nil
}

// =============================================================================
// Getters (return values, not chainable)
// =============================================================================

// groupGetID returns the group ID
// group:id() -> number
func groupGetID(L *lua.LState) int {
	group, _ := checkGroup(L)
	L.Push(lua.LNumber(group.group.ID))
	return 1
}

// groupGetName returns the group name
// group:name() -> string
func groupGetName(L *lua.LState) int {
	group, _ := checkGroup(L)
	L.Push(lua.LString(group.group.Name))
	return 1
}

// groupIsOn returns whether any light in the group is on
// group:is_on() -> bool
func groupIsOn(L *lua.LState) int {
	group, _ := checkGroup(L)
	anyOn := false
	if group.group.GroupState != nil {
		anyOn = group.group.GroupState.AnyOn
	}
	L.Push(lua.LBool(anyOn))
	return 1
}

// groupAllOn returns whether all lights in the group are on
// group:all_on() -> bool
func groupAllOn(L *lua.LState) int {
	group, _ := checkGroup(L)
	allOn := false
	if group.group.GroupState != nil {
		allOn = group.group.GroupState.AllOn
	}
	L.Push(lua.LBool(allOn))
	return 1
}

// groupAnyOn returns whether any light in the group is on
// group:any_on() -> bool
func groupAnyOn(L *lua.LState) int {
	group, _ := checkGroup(L)
	anyOn := false
	if group.group.GroupState != nil {
		anyOn = group.group.GroupState.AnyOn
	}
	L.Push(lua.LBool(anyOn))
	return 1
}

// groupGetBri gets the current brightness
// group:get_bri() -> number
func groupGetBri(L *lua.LState) int {
	group, _ := checkGroup(L)
	bri := 0
	if group.group.State != nil {
		bri = int(group.group.State.Bri)
	}
	L.Push(lua.LNumber(bri))
	return 1
}

// groupGetLights returns the light IDs in the group
// group:lights() -> table of light IDs
func groupGetLights(L *lua.LState) int {
	group, _ := checkGroup(L)
	tbl := L.NewTable()
	for i, lightID := range group.group.Lights {
		tbl.RawSetInt(i+1, lua.LString(lightID))
	}
	L.Push(tbl)
	return 1
}

// =============================================================================
// Chainable Setters (return self for chaining)
// =============================================================================

// groupOn turns the group on (chainable)
// group:on() -> self
func groupOn(L *lua.LState) int {
	group, ud := checkGroup(L)
	err := group.group.On()
	if err != nil {
		log.Error().Err(err).Int("group", group.group.ID).Msg("Failed to turn on group")
	}
	L.Push(ud)
	return 1
}

// groupOff turns the group off (chainable)
// group:off() -> self
func groupOff(L *lua.LState) int {
	group, ud := checkGroup(L)
	err := group.group.Off()
	if err != nil {
		log.Error().Err(err).Int("group", group.group.ID).Msg("Failed to turn off group")
	}
	L.Push(ud)
	return 1
}

// groupToggle toggles the group on/off (chainable)
// group:toggle() -> self
func groupToggle(L *lua.LState) int {
	group, ud := checkGroup(L)
	var err error
	anyOn := false
	if group.group.GroupState != nil {
		anyOn = group.group.GroupState.AnyOn
	}
	if anyOn {
		err = group.group.Off()
	} else {
		err = group.group.On()
	}
	if err != nil {
		log.Error().Err(err).Int("group", group.group.ID).Msg("Failed to toggle group")
	}
	L.Push(ud)
	return 1
}

// groupSetBri sets the group brightness (1-254) (chainable)
// group:set_bri(value) -> self
func groupSetBri(L *lua.LState) int {
	group, ud := checkGroup(L)
	bri := L.CheckInt(2)

	// Clamp to valid range
	if bri < 1 {
		bri = 1
	}
	if bri > 254 {
		bri = 254
	}

	err := group.group.Bri(uint8(bri))
	if err != nil {
		log.Error().Err(err).Int("group", group.group.ID).Int("bri", bri).Msg("Failed to set group brightness")
	}
	L.Push(ud)
	return 1
}

// groupSetScene activates a scene on the group (chainable)
// group:set_scene(scene_name) -> self
func groupSetScene(L *lua.LState) int {
	group, ud := checkGroup(L)
	sceneName := L.CheckString(2)

	groupID := strconv.Itoa(group.group.ID)

	// Find scene by name
	scene, err := group.sceneIndex.FindByName(sceneName, groupID)
	if err != nil {
		log.Error().Err(err).Int("group", group.group.ID).Str("scene", sceneName).Msg("Failed to find scene")
		L.Push(ud)
		return 1
	}

	err = group.group.Scene(scene.ID)
	if err != nil {
		log.Error().Err(err).Int("group", group.group.ID).Str("scene", sceneName).Msg("Failed to activate scene")
	} else {
		log.Debug().Int("group", group.group.ID).Str("scene", sceneName).Msg("Scene activated")
	}
	L.Push(ud)
	return 1
}

// groupSetColorXY sets the group color using CIE xy coordinates (chainable)
// group:set_color(x, y) -> self
func groupSetColorXY(L *lua.LState) int {
	group, ud := checkGroup(L)
	x := float32(L.CheckNumber(2))
	y := float32(L.CheckNumber(3))

	err := group.group.Xy([]float32{x, y})
	if err != nil {
		log.Error().Err(err).Int("group", group.group.ID).Msg("Failed to set group color XY")
	}
	L.Push(ud)
	return 1
}

// groupSetState sets multiple state properties at once (chainable)
// group:set_state({on = true, bri = 200, hue = 40000, sat = 254, xy = {0.5, 0.4}, ct = 300, scene = "Relax"}) -> self
func groupSetState(L *lua.LState) int {
	group, ud := checkGroup(L)
	tbl := L.CheckTable(2)

	state := huego.State{}
	hasState := false

	// scene (special handling - activate scene instead of state)
	if v := tbl.RawGetString("scene"); v != lua.LNil {
		if sceneName, ok := v.(lua.LString); ok {
			groupID := strconv.Itoa(group.group.ID)
			scene, err := group.sceneIndex.FindByName(string(sceneName), groupID)
			if err != nil {
				log.Error().Err(err).Int("group", group.group.ID).Str("scene", string(sceneName)).Msg("Failed to find scene")
			} else {
				err = group.group.Scene(scene.ID)
				if err != nil {
					log.Error().Err(err).Int("group", group.group.ID).Str("scene", string(sceneName)).Msg("Failed to activate scene")
				}
			}
			// If scene is set, we're done - scene overrides other state
			L.Push(ud)
			return 1
		}
	}

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
		err := group.group.SetState(state)
		if err != nil {
			log.Error().Err(err).Int("group", group.group.ID).Msg("Failed to set state")
		}
	}

	L.Push(ud)
	return 1
}
