package context

import (
	"context"

	lua "github.com/yuin/gopher-lua"

	"github.com/dokzlo13/lightd/internal/hue/reconcile"
)

// ReconcilerModule provides ctx:reconcile() and ctx:force_reconcile() for triggering reconciliation.
//
// This is installed at the root level (not nested), so it's called as ctx:reconcile().
// Before triggering, it flushes any pending desired state changes from builders.
//
// Example Lua usage:
//
//	ctx.desired:group("1"):on():set_scene("Relax")
//	ctx.desired:light("5"):set_bri(254)
//	ctx:reconcile() -- flushes pending and triggers the orchestrator (dirty resources only)
//	ctx:force_reconcile() -- forces reconciliation of ALL resources with desired state
type ReconcilerModule struct {
	orchestrator  *reconcile.Orchestrator
	desiredModule *DesiredModule
}

// NewReconcilerModule creates a new reconciler module.
func NewReconcilerModule(orchestrator *reconcile.Orchestrator, desiredModule *DesiredModule) *ReconcilerModule {
	return &ReconcilerModule{
		orchestrator:  orchestrator,
		desiredModule: desiredModule,
	}
}

// Name returns empty string - this module installs at root level.
func (m *ReconcilerModule) Name() string {
	return ""
}

// Install adds ctx:reconcile() and ctx:force_reconcile() to the context table.
func (m *ReconcilerModule) Install(L *lua.LState, ctx *lua.LTable) {
	// reconcile() - method syntax, arg 1 is self (ctx table itself)
	L.SetField(ctx, "reconcile", L.NewFunction(m.reconcile()))
	// force_reconcile() - forces reconciliation of ALL resources
	L.SetField(ctx, "force_reconcile", L.NewFunction(m.forceReconcile()))
}

// reconcile returns a Lua function that flushes pending and triggers the orchestrator.
func (m *ReconcilerModule) reconcile() lua.LGFunction {
	return func(L *lua.LState) int {
		// L.CheckTable(1) // self - optional, ctx:reconcile() passes ctx as self

		// Flush all pending desired state changes from builders
		if m.desiredModule != nil {
			m.desiredModule.Flush()
		}

		// Trigger orchestrator (MarkApplied is called after reconciliation completes)
		if m.orchestrator != nil {
			m.orchestrator.Trigger()
		}
		return 0
	}
}

// forceReconcile returns a Lua function that forces reconciliation of ALL resources.
// This clears caches and re-applies desired state to all resources.
func (m *ReconcilerModule) forceReconcile() lua.LGFunction {
	return func(L *lua.LState) int {
		// Flush all pending desired state changes from builders
		if m.desiredModule != nil {
			m.desiredModule.Flush()
		}

		// Trigger ALL resources for reconciliation
		if m.orchestrator != nil {
			ctx := L.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			m.orchestrator.TriggerAll(ctx)
		}
		return 0
	}
}
