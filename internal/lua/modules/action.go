package modules

import (
	"context"
	"errors"
	"fmt"
	"github.com/dokzlo13/lightd/internal/actions"
	"github.com/dokzlo13/lightd/internal/hue"
	"github.com/dokzlo13/lightd/internal/hue/bridge"
	"github.com/dokzlo13/lightd/internal/hue/reconcile"
	"github.com/dokzlo13/lightd/internal/hue/reconcile/group"
	"github.com/dokzlo13/lightd/internal/hue/reconcile/light"
	luactx "github.com/dokzlo13/lightd/internal/lua/context"
	lua "github.com/yuin/gopher-lua"
)

type ActionModule struct {
	registry     *actions.Registry
	invoker      *actions.Invoker
	newFrame     func() (*luactx.Builder, *luactx.DesiredModule)
	orchestrator *reconcile.Orchestrator
	commitGuard  func(context.Context) (func(), error)
	fatal        func(error)
}

func NewActionModule(registry *actions.Registry, bridgeClient *bridge.Client, stores *hue.StoreRegistry, orchestrator *reconcile.Orchestrator) *ActionModule {
	m := &ActionModule{registry: registry, orchestrator: orchestrator}
	if bridgeClient != nil {
		m.commitGuard = bridgeClient.AcquireCommand
	}
	actual := group.NewActualProvider(bridgeClient)
	m.newFrame = func() (*luactx.Builder, *luactx.DesiredModule) {
		desired := luactx.NewDesiredModule(stores.Groups(), stores.Lights())
		desired.SetActualProviders(actual, light.NewActualProvider(bridgeClient))
		if bridgeClient != nil {
			desired.SetCommitHooks(bridgeClient.AcquireCommand, bridgeClient.RecordDesiredTx)
		}
		b := luactx.NewBuilder().Register(luactx.NewActualModule(actual)).Register(desired).Register(luactx.NewReconcilerModule(orchestrator, desired)).Register(luactx.NewRequestModule())
		return b, desired
	}
	return m
}
func (m *ActionModule) SetFatalHandler(fn func(error)) { m.fatal = fn }
func (m *ActionModule) SetInvoker(i *actions.Invoker)  { m.invoker = i }
func (m *ActionModule) Loader(L *lua.LState) int {
	mod := L.NewTable()
	L.SetField(mod, "define", L.NewFunction(m.define))
	L.SetField(mod, "run", L.NewFunction(m.run))
	L.Push(mod)
	return 1
}
func (m *ActionModule) run(L *lua.LState) int {
	name := L.CheckString(1)
	args := RequireLuaMap(L, L.OptTable(2, L.NewTable()))
	ctx := L.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	if m.invoker == nil {
		L.RaiseError("action invoker unavailable")
		return 0
	}
	if err := m.invoker.InvokeWithSource(ctx, name, args, "", "lua", ""); err != nil {
		L.RaiseError("action %q failed: %s", name, err)
	}
	return 0
}
func (m *ActionModule) define(L *lua.LState) int {
	a := &luaAction{L: L, name: L.CheckString(1), fn: L.CheckFunction(2), module: m}
	if err := m.registry.Register(a); err != nil {
		L.RaiseError("register action: %s", err)
	}
	return 0
}

type luaAction struct {
	L      *lua.LState
	name   string
	fn     *lua.LFunction
	module *ActionModule
}

func (a *luaAction) Name() string { return a.name }
func (a *luaAction) Execute(ctx *actions.Context, args map[string]any) (err error) {
	previous := a.L.Context()
	a.L.SetContext(ctx.Ctx())
	defer func() {
		if previous == nil {
			a.L.RemoveContext()
		} else {
			a.L.SetContext(previous)
		}
	}()
	builder, desired := a.module.newFrame()
	defer desired.Close()
	// Each nested invocation has its own frame. Only a successful final outcome
	// commits this frame; explicit ctx:reconcile checkpoints remain independent.
	ctx.SetCommitGuard(a.module.commitGuard)
	ctx.DeferCommit(desired.FlushTx, func() {
		if a.module.orchestrator != nil {
			a.module.orchestrator.Trigger()
		}
	})
	ctxTable := builder.Build(a.L)
	argsTable := MapToLuaTable(a.L, args)
	if err = a.L.CallByParam(lua.P{Fn: a.fn, NRet: 0, Protect: true}, ctxTable, argsTable); err != nil {
		var apiErr *lua.ApiError
		if errors.As(err, &apiErr) && apiErr.Type == lua.ApiErrorPanic {
			if a.module.fatal != nil {
				a.module.fatal(err)
			}
			return errors.Join(actions.ErrIntegrity, err)
		}
		return fmt.Errorf("Lua action %s: %w", a.name, err)
	}
	return nil
}
