package modules

import (
	"github.com/rs/zerolog/log"
	lua "github.com/yuin/gopher-lua"

	"github.com/dokzlo13/lightd/internal/hue"
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
type HueModule struct {
	client *hue.Client
	cache  *hue.GroupCache
}

// NewHueModule creates a new hue module
func NewHueModule(client *hue.Client, cache *hue.GroupCache) *HueModule {
	return &HueModule{
		client: client,
		cache:  cache,
	}
}

// Loader is the module loader for Lua
func (m *HueModule) Loader(L *lua.LState) int {
	mod := L.NewTable()

	// hue.cache
	cache := L.NewTable()
	L.SetField(cache, "group", L.NewFunction(m.cacheGroup))
	L.SetField(mod, "cache", cache)

	// hue.set_group_brightness(group_id, brightness) -> (ok, err)
	L.SetField(mod, "set_group_brightness", L.NewFunction(m.setGroupBrightness))

	// hue.adjust_group_brightness(group_id, delta) -> (ok, err)
	L.SetField(mod, "adjust_group_brightness", L.NewFunction(m.adjustGroupBrightness))

	// hue.recall_scene(group_id, scene_name) -> (ok, err)
	L.SetField(mod, "recall_scene", L.NewFunction(m.recallScene))

	// hue.get_group_brightness(group_id) -> (brightness, err)
	L.SetField(mod, "get_group_brightness", L.NewFunction(m.getGroupBrightness))

	L.Push(mod)
	return 1
}

// cacheGroup(group_id) -> (state_table, err)
// Returns cached state if fresh, otherwise fetches from bridge and caches.
func (m *HueModule) cacheGroup(L *lua.LState) int {
	groupID := L.CheckString(1)

	// Check cache first
	if state := m.cache.Get(groupID); state != nil {
		tbl := L.NewTable()
		L.SetField(tbl, "all_on", lua.LBool(state.AllOn))
		L.SetField(tbl, "any_on", lua.LBool(state.AnyOn))
		L.Push(tbl)
		L.Push(lua.LNil)
		return 2
	}

	// Cache miss or stale - fetch from bridge
	ctx := L.Context()
	group, err := m.client.GetGroup(ctx, groupID)
	if err != nil {
		log.Error().Err(err).Str("group", groupID).Msg("Failed to get group state")
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
	}

	// Cache the result
	m.cache.Set(groupID, group.State)

	tbl := L.NewTable()
	L.SetField(tbl, "all_on", lua.LBool(group.State.AllOn))
	L.SetField(tbl, "any_on", lua.LBool(group.State.AnyOn))

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

	ctx := L.Context()

	err := m.client.SetGroupAction(ctx, groupID, map[string]interface{}{
		"bri": brightness,
	})
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

	ctx := L.Context()

	// Fetch current brightness
	group, err := m.client.GetGroup(ctx, groupID)
	if err != nil {
		log.Error().Err(err).Str("group", groupID).Msg("Failed to get group for brightness adjustment")
		L.Push(lua.LBool(false))
		L.Push(lua.LString(err.Error()))
		return 2
	}

	currentBri := group.Action.Bri
	newBri := currentBri + delta

	// Clamp to valid range
	if newBri < 1 {
		newBri = 1
	}
	if newBri > 254 {
		newBri = 254
	}

	err = m.client.SetGroupAction(ctx, groupID, map[string]interface{}{
		"bri": newBri,
	})
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

	ctx := L.Context()

	group, err := m.client.GetGroup(ctx, groupID)
	if err != nil {
		log.Error().Err(err).Str("group", groupID).Msg("Failed to get group brightness")
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
	}

	L.Push(lua.LNumber(group.Action.Bri))
	L.Push(lua.LNil)
	return 2
}

// recallScene(group_id, scene_name) -> (ok, err)
// Activates a scene on a group
func (m *HueModule) recallScene(L *lua.LState) int {
	groupID := L.CheckString(1)
	sceneName := L.CheckString(2)

	ctx := L.Context()

	// Find scene by name first (ActivateScene expects scene ID, not name)
	scene, err := m.client.FindSceneByName(ctx, sceneName, groupID)
	if err != nil {
		log.Error().Err(err).Str("group", groupID).Str("scene", sceneName).Msg("Failed to find scene")
		L.Push(lua.LBool(false))
		L.Push(lua.LString(err.Error()))
		return 2
	}

	err = m.client.ActivateScene(ctx, groupID, scene.ID)
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
