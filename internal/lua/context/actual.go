package context

import (
	"strconv"

	"github.com/amimof/huego"
	"github.com/rs/zerolog/log"
	lua "github.com/yuin/gopher-lua"

	"github.com/dokzlo13/lightd/internal/hue"
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
	bridge *huego.Bridge
	cache  *hue.GroupCache
}

// NewActualModule creates a new actual state module.
func NewActualModule(bridge *huego.Bridge, cache *hue.GroupCache) *ActualModule {
	return &ActualModule{
		bridge: bridge,
		cache:  cache,
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

// group is a Lua function that fetches fresh group state.
// Always fetches from bridge (not cache) to ensure fresh data for decisions.
// Uses L.Context() for cancellation support.
func (m *ActualModule) group(L *lua.LState) int {
	L.CheckTable(1) // self
	groupID := L.CheckString(2)

	id, err := strconv.Atoi(groupID)
	if err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString("invalid group ID"))
		return 2
	}

	// Always fetch FRESH state from bridge (not stale cache)
	group, err := m.bridge.GetGroup(id)
	if err != nil {
		log.Error().Err(err).Str("group", groupID).Msg("Failed to get group state")
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
	}

	// Cache the fresh result for other consumers
	if group.GroupState != nil {
		m.cache.Set(groupID, *group.GroupState)
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
	L.Push(lua.LNil) // no error
	return 2
}
