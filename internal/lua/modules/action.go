package modules

import (
	"context"

	"github.com/amimof/huego"
	"github.com/rs/zerolog/log"
	lua "github.com/yuin/gopher-lua"

	"github.com/dokzlo13/lightd/internal/actions"
	"github.com/dokzlo13/lightd/internal/hue"
	"github.com/dokzlo13/lightd/internal/hue/reconcile"
	"github.com/dokzlo13/lightd/internal/hue/reconcile/group"
	luactx "github.com/dokzlo13/lightd/internal/lua/context"
)

// actionContext holds common dependencies for Lua actions.
//
// ARCHITECTURE NOTE: The *lua.LState (L) is stored at action registration time.
// This works because:
//  1. All Lua execution is single-threaded through the Runtime worker goroutine
//  2. The LState is never recreated during the Runtime's lifetime
//  3. Context modules use L.Context() to get the Go context for cancellation
//
// If the architecture changes to support multiple LStates (e.g., for testing),
// this coupling would need to be revisited.
type actionContext struct {
	L              *lua.LState
	contextBuilder *luactx.Builder
}

// createContextTable creates the ctx table passed to Lua action functions.
// Uses the context builder pattern for modular context construction.
// Context modules use L.Context() internally for cancellation support.
func (a *actionContext) createContextTable() *lua.LTable {
	return a.contextBuilder.Build(a.L)
}

// ActionModule provides action.define() to Lua
type ActionModule struct {
	registry       *actions.Registry
	contextBuilder *luactx.Builder
}

// NewActionModule creates a new action module.
// It registers all context modules that will be available to Lua actions.
func NewActionModule(
	registry *actions.Registry,
	bridge *huego.Bridge,
	storeRegistry *hue.StoreRegistry,
	orchestrator *reconcile.Orchestrator,
) *ActionModule {
	// Create the GroupActualProvider for actual state access
	actualProvider := group.NewActualProvider(bridge)

	// Create the desired module (shared between context and reconciler for flush)
	desiredModule := luactx.NewDesiredModule(storeRegistry.Groups(), storeRegistry.Lights())

	// Build the context builder with all modules
	builder := luactx.NewBuilder().
		Register(luactx.NewActualModule(actualProvider)).
		Register(desiredModule).
		Register(luactx.NewReconcilerModule(orchestrator, desiredModule)).
		Register(luactx.NewRequestModule())

	return &ActionModule{
		registry:       registry,
		contextBuilder: builder,
	}
}

// Loader is the module loader for Lua
func (m *ActionModule) Loader(L *lua.LState) int {
	mod := L.NewTable()

	L.SetField(mod, "define", L.NewFunction(m.define))
	L.SetField(mod, "run", L.NewFunction(m.run))

	L.Push(mod)
	return 1
}

// run(name, args) - Run an action immediately (useful for startup)
// Note: This bypasses the ledger/deduplication, use for initialization only
func (m *ActionModule) run(L *lua.LState) int {
	name := L.CheckString(1)
	argsTable := L.OptTable(2, L.NewTable())
	args := LuaTableToMap(argsTable)

	action, exists := m.registry.Get(name)
	if !exists {
		L.RaiseError("action %q not found", name)
		return 0
	}

	// Ensure L has a valid context (may be nil during script loading)
	ctx := L.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	// Set the context on L so modules can access it
	L.SetContext(ctx)

	// Create a minimal action context
	actx := actions.NewContext(ctx, nil, nil, nil, nil)

	log.Debug().Str("action", name).Msg("Running action from Lua")

	if err := action.Execute(actx, args); err != nil {
		L.RaiseError("action %q failed: %s", name, err.Error())
		return 0
	}

	return 0
}

// define(name, function) - Define an action
func (m *ActionModule) define(L *lua.LState) int {
	name := L.CheckString(1)
	fn := L.CheckFunction(2)

	action := &luaAction{
		actionContext: actionContext{
			L:              L,
			contextBuilder: m.contextBuilder,
		},
		name: name,
		fn:   fn,
	}

	if err := m.registry.Register(action); err != nil {
		L.RaiseError("failed to register action: %s", err.Error())
		return 0
	}

	return 0
}

// luaAction wraps a Lua function as an action
type luaAction struct {
	actionContext
	name string
	fn   *lua.LFunction
}

func (a *luaAction) Name() string { return a.name }

func (a *luaAction) Execute(ctx *actions.Context, args map[string]any) error {
	// Update LState context to include request data from webhook triggers
	a.L.SetContext(ctx.Ctx())

	// Ensure pending state is flushed after action completes (even without ctx:reconcile())
	defer a.contextBuilder.Cleanup()

	ctxTable := a.createContextTable()
	argsTable := MapToLuaTable(a.L, args)

	a.L.Push(a.fn)
	a.L.Push(ctxTable)
	a.L.Push(argsTable)

	if err := a.L.PCall(2, 0, nil); err != nil {
		return err
	}

	return nil
}
