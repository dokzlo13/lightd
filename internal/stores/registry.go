// Package stores provides centralized access to typed state stores.
package stores

import (
	"github.com/dokzlo13/lightd/internal/reconcile"
	"github.com/dokzlo13/lightd/internal/reconcile/group"
	"github.com/dokzlo13/lightd/internal/reconcile/light"
	"github.com/dokzlo13/lightd/internal/state"
)

// Registry provides centralized access to all typed stores.
// This replaces passing individual stores throughout the codebase.
type Registry struct {
	base       *state.Store
	groupStore *state.TypedStore[group.Desired]
	lightStore *state.TypedStore[light.Desired]
}

// NewRegistry creates a new store registry with typed stores for each resource kind.
func NewRegistry(base *state.Store) *Registry {
	return &Registry{
		base:       base,
		groupStore: state.NewTypedStore[group.Desired](base, string(reconcile.KindGroup)),
		lightStore: state.NewTypedStore[light.Desired](base, string(reconcile.KindLight)),
	}
}

// Groups returns the typed store for group desired state.
func (r *Registry) Groups() *state.TypedStore[group.Desired] {
	return r.groupStore
}

// Lights returns the typed store for light desired state.
func (r *Registry) Lights() *state.TypedStore[light.Desired] {
	return r.lightStore
}

// Clear removes all state from all stores.
func (r *Registry) Clear() error {
	if err := r.groupStore.Clear(); err != nil {
		return err
	}
	return r.lightStore.Clear()
}
