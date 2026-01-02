package context

import (
	"github.com/rs/zerolog/log"
	lua "github.com/yuin/gopher-lua"

	"github.com/dokzlo13/lightd/internal/hue/reconcile/group"
)

// ActualModule provides ctx.actual for accessing current Hue state.
//
// Methods use : syntax in Lua, so arg 1 is self, arg 2+ are real args.
// Returns two values: (result, error_string) so Lua can distinguish errors.
//
// Example Lua usage:
//
//	local state, err = ctx.actual:group("1")
//	if err then
//	    log.error("Failed: " .. err)
//	else
//	    if state.any_on then ... end
//	end
type ActualModule struct {
	provider *group.ActualProvider
}

// NewActualModule creates a new actual state module.
func NewActualModule(provider *group.ActualProvider) *ActualModule {
	return &ActualModule{
		provider: provider,
	}
}

// Name returns "actual" - the field name in ctx.
func (m *ActualModule) Name() string {
	return "actual"
}

// Install adds ctx.actual to the context table.
func (m *ActualModule) Install(L *lua.LState, ctx *lua.LTable) {
	actual := L.NewTable()

	// actual:group(group_id) -> (state_table, err)
	L.SetField(actual, "group", L.NewFunction(m.group))

	L.SetField(ctx, m.Name(), actual)
}

// group is a Lua function that fetches fresh group state from the bridge.
// Uses L.Context() for cancellation support.
func (m *ActualModule) group(L *lua.LState) int {
	L.CheckTable(1) // self
	groupID := L.CheckString(2)

	// Get the context from LState
	ctx := L.Context()

	// Fetch fresh state from bridge
	state, err := m.provider.Get(ctx, groupID)
	if err != nil {
		log.Error().Err(err).Str("group", groupID).Msg("Failed to get group state")
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
	}

	tbl := L.NewTable()
	L.SetField(tbl, "all_on", lua.LBool(state.AllOn))
	L.SetField(tbl, "any_on", lua.LBool(state.AnyOn))
	L.Push(tbl)
	L.Push(lua.LNil) // no error
	return 2
}

// TODO: Add actual:light(light_id) -> (state_table, err)
