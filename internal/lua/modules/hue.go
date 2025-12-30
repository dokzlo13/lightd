package modules

import (
	"strconv"

	"github.com/amimof/huego"
	"github.com/rs/zerolog/log"
	lua "github.com/yuin/gopher-lua"

	"github.com/dokzlo13/lightd/internal/cache"
)

// HueModule provides hue.* functions to Lua.
//
// ERROR HANDLING CONVENTION:
// All functions that can fail return two values: (result, error_string).
//   - On success: (result, nil)
//   - On error: (nil/false, "error message")
//
// This allows Lua code to distinguish between errors and legitimate nil/false values.
// Example Lua usage:
//
//	local result, err = hue.get_group_brightness("1")
//	if err then
//	    log.error("Failed: " .. err)
//	else
//	    log.info("Brightness: " .. result)
//	end
//
// NEW: Chainable, object-oriented API for fluent light/group control:
//
//	local lamp = hue.light("5")
//	lamp:on():set_bri(200):set_color(0.45, 0.41)
//
//	local group = hue.group("2")
//	group:set_scene("Relax")
//	group:off()
//
//	-- Or generic state setting (highly extensible):
//	lamp:set_state({
//	    on = true,
//	    bri = 200,
//	    hue = 40000,
//	})
type HueModule struct {
	bridge     *huego.Bridge
	sceneIndex *cache.SceneIndex
}

// NewHueModule creates a new hue module
func NewHueModule(bridge *huego.Bridge, sceneIndex *cache.SceneIndex) *HueModule {
	return &HueModule{
		bridge:     bridge,
		sceneIndex: sceneIndex,
	}
}

// Loader is the module loader for Lua
func (m *HueModule) Loader(L *lua.LState) int {
	// Register userdata metatables
	RegisterLightType(L)
	RegisterGroupType(L)

	mod := L.NewTable()

	// hue.get_group - fetch fresh group state (no caching)
	L.SetField(mod, "get_group_state", L.NewFunction(m.getGroupState))

	// Legacy functions (keep for backward compatibility)
	L.SetField(mod, "set_group_brightness", L.NewFunction(m.setGroupBrightness))
	L.SetField(mod, "adjust_group_brightness", L.NewFunction(m.adjustGroupBrightness))
	L.SetField(mod, "recall_scene", L.NewFunction(m.recallScene))
	L.SetField(mod, "get_group_brightness", L.NewFunction(m.getGroupBrightness))

	// NEW: Factory methods for userdata-based API
	L.SetField(mod, "light", L.NewFunction(m.getLight))
	L.SetField(mod, "lights", L.NewFunction(m.getLights))
	L.SetField(mod, "group", L.NewFunction(m.getGroup))
	L.SetField(mod, "groups", L.NewFunction(m.getGroups))

	L.Push(mod)
	return 1
}

// =============================================================================
// Factory Methods (return userdata)
// =============================================================================

// getLight(id) -> (light_userdata, err)
// Returns a light userdata by ID (accepts string or number)
func (m *HueModule) getLight(L *lua.LState) int {
	var lightID int
	var err error

	// Accept both string and number
	switch v := L.Get(1).(type) {
	case lua.LString:
		lightID, err = strconv.Atoi(string(v))
		if err != nil {
			L.Push(lua.LNil)
			L.Push(lua.LString("invalid light ID: " + string(v)))
			return 2
		}
	case lua.LNumber:
		lightID = int(v)
	default:
		L.ArgError(1, "light ID must be string or number")
		return 0
	}

	light, err := m.bridge.GetLight(lightID)
	if err != nil {
		log.Error().Err(err).Int("light", lightID).Msg("Failed to get light")
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
	}

	pushLight(L, light)
	L.Push(lua.LNil)
	return 2
}

// getLights() -> (table of light_userdata, err)
// Returns all lights as userdata
func (m *HueModule) getLights(L *lua.LState) int {
	lights, err := m.bridge.GetLights()
	if err != nil {
		log.Error().Err(err).Msg("Failed to get lights")
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
	}

	tbl := L.NewTable()
	for i := range lights {
		pushLight(L, &lights[i])
		tbl.RawSetInt(i+1, L.Get(-1))
		L.Pop(1)
	}

	L.Push(tbl)
	L.Push(lua.LNil)
	return 2
}

// getGroup(id) -> (group_userdata, err)
// id can be string or number
func (m *HueModule) getGroup(L *lua.LState) int {
	var groupID int
	var err error

	// Accept both string and number
	switch v := L.Get(1).(type) {
	case lua.LString:
		groupID, err = strconv.Atoi(string(v))
		if err != nil {
			L.Push(lua.LNil)
			L.Push(lua.LString("invalid group ID: " + string(v)))
			return 2
		}
	case lua.LNumber:
		groupID = int(v)
	default:
		L.ArgError(1, "group ID must be string or number")
		return 0
	}

	group, err := m.bridge.GetGroup(groupID)
	if err != nil {
		log.Error().Err(err).Int("group", groupID).Msg("Failed to get group")
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
	}

	pushGroup(L, group, m.sceneIndex)
	L.Push(lua.LNil)
	return 2
}

// getGroups() -> (table of group_userdata, err)
// Returns all groups as userdata
func (m *HueModule) getGroups(L *lua.LState) int {
	groups, err := m.bridge.GetGroups()
	if err != nil {
		log.Error().Err(err).Msg("Failed to get groups")
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
	}

	tbl := L.NewTable()
	for i := range groups {
		pushGroup(L, &groups[i], m.sceneIndex)
		tbl.RawSetInt(i+1, L.Get(-1))
		L.Pop(1)
	}

	L.Push(tbl)
	L.Push(lua.LNil)
	return 2
}

// =============================================================================
// Legacy Functions (kept for backward compatibility)
// =============================================================================

// getGroupState(group_id) -> (state_table, err)
// Fetches fresh group state from the bridge.
func (m *HueModule) getGroupState(L *lua.LState) int {
	groupID := L.CheckString(1)

	id, err := strconv.Atoi(groupID)
	if err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString("invalid group ID"))
		return 2
	}

	group, err := m.bridge.GetGroup(id)
	if err != nil {
		log.Error().Err(err).Str("group", groupID).Msg("Failed to get group state")
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
	}

	tbl := L.NewTable()
	allOn := false
	anyOn := false
	if group.GroupState != nil {
		allOn = group.GroupState.AllOn
		anyOn = group.GroupState.AnyOn
	}
	L.SetField(tbl, "all_on", lua.LBool(allOn))
	L.SetField(tbl, "any_on", lua.LBool(anyOn))

	L.Push(tbl)
	L.Push(lua.LNil)
	return 2
}

// setGroupBrightness(group_id, brightness) -> (ok, err)
// Sets group brightness (0-254)
func (m *HueModule) setGroupBrightness(L *lua.LState) int {
	groupID := L.CheckString(1)
	brightness := L.CheckInt(2)

	// Clamp to valid range
	if brightness < 1 {
		brightness = 1
	}
	if brightness > 254 {
		brightness = 254
	}

	id, err := strconv.Atoi(groupID)
	if err != nil {
		L.Push(lua.LBool(false))
		L.Push(lua.LString("invalid group ID"))
		return 2
	}

	group, err := m.bridge.GetGroup(id)
	if err != nil {
		log.Error().Err(err).Str("group", groupID).Int("bri", brightness).Msg("Failed to get group")
		L.Push(lua.LBool(false))
		L.Push(lua.LString(err.Error()))
		return 2
	}

	err = group.Bri(uint8(brightness))
	if err != nil {
		log.Error().Err(err).Str("group", groupID).Int("bri", brightness).Msg("Failed to set group brightness")
		L.Push(lua.LBool(false))
		L.Push(lua.LString(err.Error()))
		return 2
	}

	log.Debug().Str("group", groupID).Int("bri", brightness).Msg("Set group brightness")
	L.Push(lua.LBool(true))
	L.Push(lua.LNil)
	return 2
}

// adjustGroupBrightness(group_id, delta) -> (ok, err)
// Adjusts group brightness by delta
func (m *HueModule) adjustGroupBrightness(L *lua.LState) int {
	groupID := L.CheckString(1)
	delta := L.CheckInt(2)

	id, err := strconv.Atoi(groupID)
	if err != nil {
		L.Push(lua.LBool(false))
		L.Push(lua.LString("invalid group ID"))
		return 2
	}

	// Fetch current brightness
	group, err := m.bridge.GetGroup(id)
	if err != nil {
		log.Error().Err(err).Str("group", groupID).Msg("Failed to get group for brightness adjustment")
		L.Push(lua.LBool(false))
		L.Push(lua.LString(err.Error()))
		return 2
	}

	currentBri := 0
	if group.State != nil {
		currentBri = int(group.State.Bri)
	}
	newBri := currentBri + delta

	// Clamp to valid range
	if newBri < 1 {
		newBri = 1
	}
	if newBri > 254 {
		newBri = 254
	}

	err = group.Bri(uint8(newBri))
	if err != nil {
		log.Error().Err(err).Str("group", groupID).Int("bri", newBri).Msg("Failed to adjust group brightness")
		L.Push(lua.LBool(false))
		L.Push(lua.LString(err.Error()))
		return 2
	}

	log.Debug().Str("group", groupID).Int("old_bri", currentBri).Int("new_bri", newBri).Int("delta", delta).Msg("Adjusted group brightness")
	L.Push(lua.LBool(true))
	L.Push(lua.LNil)
	return 2
}

// getGroupBrightness(group_id) -> (brightness, err)
// Gets current group brightness (0-254)
func (m *HueModule) getGroupBrightness(L *lua.LState) int {
	groupID := L.CheckString(1)

	id, err := strconv.Atoi(groupID)
	if err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString("invalid group ID"))
		return 2
	}

	group, err := m.bridge.GetGroup(id)
	if err != nil {
		log.Error().Err(err).Str("group", groupID).Msg("Failed to get group brightness")
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
	}

	bri := 0
	if group.State != nil {
		bri = int(group.State.Bri)
	}

	L.Push(lua.LNumber(bri))
	L.Push(lua.LNil)
	return 2
}

// recallScene(group_id, scene_name) -> (ok, err)
// Activates a scene on a group
func (m *HueModule) recallScene(L *lua.LState) int {
	groupID := L.CheckString(1)
	sceneName := L.CheckString(2)

	id, err := strconv.Atoi(groupID)
	if err != nil {
		L.Push(lua.LBool(false))
		L.Push(lua.LString("invalid group ID"))
		return 2
	}

	// Find scene by name first
	scene, err := m.sceneIndex.FindByName(sceneName, groupID)
	if err != nil {
		log.Error().Err(err).Str("group", groupID).Str("scene", sceneName).Msg("Failed to find scene")
		L.Push(lua.LBool(false))
		L.Push(lua.LString(err.Error()))
		return 2
	}

	// Get group and activate scene
	group, err := m.bridge.GetGroup(id)
	if err != nil {
		log.Error().Err(err).Str("group", groupID).Msg("Failed to get group")
		L.Push(lua.LBool(false))
		L.Push(lua.LString(err.Error()))
		return 2
	}

	err = group.Scene(scene.ID)
	if err != nil {
		log.Error().Err(err).Str("group", groupID).Str("scene", sceneName).Str("scene_id", scene.ID).Msg("Failed to recall scene")
		L.Push(lua.LBool(false))
		L.Push(lua.LString(err.Error()))
		return 2
	}

	log.Debug().Str("group", groupID).Str("scene", sceneName).Str("scene_id", scene.ID).Msg("Recalled scene")
	L.Push(lua.LBool(true))
	L.Push(lua.LNil)
	return 2
}
