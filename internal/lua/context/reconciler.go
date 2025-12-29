package context

import (
	lua "github.com/yuin/gopher-lua"

	"github.com/dokzlo13/lightd/internal/reconcile"
)

// ReconcilerModule provides ctx:reconcile() for triggering reconciliation.
//
// This is installed at the root level (not nested), so it's called as ctx:reconcile().
//
// Example Lua usage:
//
//	ctx.desired:set_power("1", true)
//	ctx:reconcile() -- triggers the reconciler to apply changes
type ReconcilerModule struct {
	reconciler *reconcile.Reconciler
}

// NewReconcilerModule creates a new reconciler module.
func NewReconcilerModule(reconciler *reconcile.Reconciler) *ReconcilerModule {
	return &ReconcilerModule{
		reconciler: reconciler,
	}
}

// Name returns empty string - this module installs at root level.
func (m *ReconcilerModule) Name() string {
	return ""
}

// Install adds ctx:reconcile() to the context table.
func (m *ReconcilerModule) Install(L *lua.LState, ctx *lua.LTable) {
	// reconcile() - method syntax, arg 1 is self (ctx table itself)
	L.SetField(ctx, "reconcile", L.NewFunction(m.reconcile()))
}

// reconcile returns a Lua function that triggers the reconciler.
func (m *ReconcilerModule) reconcile() lua.LGFunction {
	return func(L *lua.LState) int {
		// L.CheckTable(1) // self - optional, ctx:reconcile() passes ctx as self
		if m.reconciler != nil {
			m.reconciler.Trigger()
		}
		return 0
	}
}
