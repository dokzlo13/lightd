package context

import (
	"github.com/rs/zerolog/log"
	lua "github.com/yuin/gopher-lua"

	"github.com/dokzlo13/lightd/internal/state"
)

// DesiredModule provides ctx.desired for accessing/modifying desired state.
//
// Methods use : syntax in Lua, so arg 1 is self, arg 2+ are real args.
//
// Example Lua usage:
//
//	ctx.desired:set_bank("1", "Bright")
//	ctx.desired:set_power("1", true)
//	local bank = ctx.desired:get_bank("1")
type DesiredModule struct {
	store *state.DesiredStore
}

// NewDesiredModule creates a new desired state module.
func NewDesiredModule(store *state.DesiredStore) *DesiredModule {
	return &DesiredModule{
		store: store,
	}
}

// Name returns "desired" - the field name in ctx.
func (m *DesiredModule) Name() string {
	return "desired"
}

// Install adds ctx.desired to the context table.
func (m *DesiredModule) Install(L *lua.LState, ctx *lua.LTable) {
	desired := L.NewTable()

	// desired:set_bank(group_id, scene_name)
	L.SetField(desired, "set_bank", L.NewFunction(m.setBank()))

	// desired:set_power(group_id, on)
	L.SetField(desired, "set_power", L.NewFunction(m.setPower()))

	// desired:get_bank(group_id) -> bank_name or nil
	L.SetField(desired, "get_bank", L.NewFunction(m.getBank()))

	L.SetField(ctx, m.Name(), desired)
}

// setBank returns a Lua function that sets the bank (scene) for a group.
func (m *DesiredModule) setBank() lua.LGFunction {
	return func(L *lua.LState) int {
		L.CheckTable(1) // self
		groupID := L.CheckString(2)
		sceneName := L.CheckString(3)

		if err := m.store.SetBank(groupID, sceneName); err != nil {
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

		if err := m.store.SetPower(groupID, on); err != nil {
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

		bank := m.store.GetBank(groupID)
		if bank == "" {
			L.Push(lua.LNil)
		} else {
			L.Push(lua.LString(bank))
		}
		return 1
	}
}
