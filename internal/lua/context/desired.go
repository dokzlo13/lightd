package context

import (
	"github.com/rs/zerolog/log"
	lua "github.com/yuin/gopher-lua"

	"github.com/dokzlo13/lightd/internal/reconcile/group"
	"github.com/dokzlo13/lightd/internal/reconcile/light"
	"github.com/dokzlo13/lightd/internal/state"
)

// DesiredModule provides ctx.desired for accessing/modifying desired state.
//
// Supports two API styles:
//
// Legacy direct methods:
//
//	ctx.desired:set_bank("1", "Bright")
//	ctx.desired:set_power("1", true)
//
// New chainable builder API:
//
//	ctx.desired:group("1"):on():set_scene("Relax")
//	ctx.desired:light("5"):on():set_bri(254)
//	ctx:reconcile()  -- flushes pending and triggers reconciler
type DesiredModule struct {
	groupStore *state.TypedStore[group.Desired]
	lightStore *state.TypedStore[light.Desired]

	// Pending builders (keyed by ID)
	pendingGroups map[string]*GroupDesiredBuilder
	pendingLights map[string]*LightDesiredBuilder
}

// NewDesiredModule creates a new desired state module.
func NewDesiredModule(
	groupStore *state.TypedStore[group.Desired],
	lightStore *state.TypedStore[light.Desired],
) *DesiredModule {
	return &DesiredModule{
		groupStore:    groupStore,
		lightStore:    lightStore,
		pendingGroups: make(map[string]*GroupDesiredBuilder),
		pendingLights: make(map[string]*LightDesiredBuilder),
	}
}

// Name returns "desired" - the field name in ctx.
func (m *DesiredModule) Name() string {
	return "desired"
}

// Install adds ctx.desired to the context table.
func (m *DesiredModule) Install(L *lua.LState, ctx *lua.LTable) {
	// Register builder metatables
	RegisterGroupBuilderType(L)
	RegisterLightBuilderType(L)

	desired := L.NewTable()

	// New chainable builder factories
	L.SetField(desired, "group", L.NewFunction(m.getGroupBuilder()))
	L.SetField(desired, "light", L.NewFunction(m.getLightBuilder()))

	// Legacy methods (kept for backward compatibility)
	L.SetField(desired, "set_bank", L.NewFunction(m.setBank()))
	L.SetField(desired, "set_power", L.NewFunction(m.setPower()))
	L.SetField(desired, "get_bank", L.NewFunction(m.getBank()))
	L.SetField(desired, "has_bank", L.NewFunction(m.hasBank()))

	L.SetField(ctx, m.Name(), desired)
}

// markGroupPending marks a group builder as having pending changes.
func (m *DesiredModule) markGroupPending(builder *GroupDesiredBuilder) {
	m.pendingGroups[builder.groupID] = builder
}

// markLightPending marks a light builder as having pending changes.
func (m *DesiredModule) markLightPending(builder *LightDesiredBuilder) {
	m.pendingLights[builder.lightID] = builder
}

// Flush writes all pending builder states to stores and clears pending.
func (m *DesiredModule) Flush() error {
	if len(m.pendingGroups) == 0 && len(m.pendingLights) == 0 {
		return nil
	}

	log.Debug().
		Int("groups", len(m.pendingGroups)).
		Int("lights", len(m.pendingLights)).
		Msg("Flushing desired state")

	// Flush pending groups
	for id, b := range m.pendingGroups {
		err := m.groupStore.Update(id, func(current group.Desired) group.Desired {
			// Merge builder state into current state
			if b.state.Power != nil {
				current.Power = b.state.Power
			}
			if b.state.SceneName != "" {
				current.SceneName = b.state.SceneName
			}
			if b.state.Bri != nil {
				current.Bri = b.state.Bri
			}
			if b.state.Hue != nil {
				current.Hue = b.state.Hue
			}
			if b.state.Sat != nil {
				current.Sat = b.state.Sat
			}
			if b.state.Xy != nil {
				current.Xy = b.state.Xy
			}
			if b.state.Ct != nil {
				current.Ct = b.state.Ct
			}
			return current
		})
		if err != nil {
			log.Error().Err(err).Str("group", id).Msg("Failed to flush group desired state")
		}
	}

	// Flush pending lights
	for id, b := range m.pendingLights {
		err := m.lightStore.Update(id, func(current light.Desired) light.Desired {
			// Merge builder state into current state
			if b.state.Power != nil {
				current.Power = b.state.Power
			}
			if b.state.Bri != nil {
				current.Bri = b.state.Bri
			}
			if b.state.Hue != nil {
				current.Hue = b.state.Hue
			}
			if b.state.Sat != nil {
				current.Sat = b.state.Sat
			}
			if b.state.Xy != nil {
				current.Xy = b.state.Xy
			}
			if b.state.Ct != nil {
				current.Ct = b.state.Ct
			}
			return current
		})
		if err != nil {
			log.Error().Err(err).Str("light", id).Msg("Failed to flush light desired state")
		}
	}

	// Clear pending
	m.pendingGroups = make(map[string]*GroupDesiredBuilder)
	m.pendingLights = make(map[string]*LightDesiredBuilder)

	return nil
}

// Cleanup implements CleanupModule interface.
// Called after every action to ensure pending state is persisted even if ctx:reconcile() wasn't called.
func (m *DesiredModule) Cleanup() {
	m.Flush()
}

// getGroupBuilder returns a Lua function that creates a group builder.
func (m *DesiredModule) getGroupBuilder() lua.LGFunction {
	return func(L *lua.LState) int {
		L.CheckTable(1) // self
		groupID := L.CheckString(2)

		pushGroupBuilder(L, groupID, m)
		return 1
	}
}

// getLightBuilder returns a Lua function that creates a light builder.
func (m *DesiredModule) getLightBuilder() lua.LGFunction {
	return func(L *lua.LState) int {
		L.CheckTable(1) // self
		lightID := L.CheckString(2)

		pushLightBuilder(L, lightID, m)
		return 1
	}
}

// setBank returns a Lua function that sets the bank (scene) for a group.
func (m *DesiredModule) setBank() lua.LGFunction {
	return func(L *lua.LState) int {
		L.CheckTable(1) // self
		groupID := L.CheckString(2)
		sceneName := L.CheckString(3)

		err := m.groupStore.Update(groupID, func(current group.Desired) group.Desired {
			current.SceneName = sceneName
			return current
		})
		if err != nil {
			log.Error().Err(err).
				Str("group", groupID).
				Str("scene", sceneName).
				Msg("Failed to set bank in desired state")
		}
		return 0
	}
}

// setPower returns a Lua function that sets the power state for a group.
func (m *DesiredModule) setPower() lua.LGFunction {
	return func(L *lua.LState) int {
		L.CheckTable(1) // self
		groupID := L.CheckString(2)
		on := L.CheckBool(3)

		err := m.groupStore.Update(groupID, func(current group.Desired) group.Desired {
			current.Power = &on
			return current
		})
		if err != nil {
			log.Error().Err(err).
				Str("group", groupID).
				Bool("on", on).
				Msg("Failed to set power in desired state")
		}
		return 0
	}
}

// getBank returns a Lua function that gets the current bank for a group.
func (m *DesiredModule) getBank() lua.LGFunction {
	return func(L *lua.LState) int {
		L.CheckTable(1) // self
		groupID := L.CheckString(2)

		state, _, err := m.groupStore.Get(groupID)
		if err != nil {
			log.Error().Err(err).Str("group", groupID).Msg("Failed to get bank")
			L.Push(lua.LNil)
			return 1
		}

		if state.SceneName == "" {
			L.Push(lua.LNil)
		} else {
			L.Push(lua.LString(state.SceneName))
		}
		return 1
	}
}

// hasBank returns a Lua function that checks if a group has a bank set.
func (m *DesiredModule) hasBank() lua.LGFunction {
	return func(L *lua.LState) int {
		L.CheckTable(1) // self
		groupID := L.CheckString(2)

		state, _, err := m.groupStore.Get(groupID)
		if err != nil {
			L.Push(lua.LFalse)
			return 1
		}

		L.Push(lua.LBool(state.SceneName != ""))
		return 1
	}
}
