package hue

import (
	"github.com/dokzlo13/lightd/internal/hue/reconcile"
	"github.com/dokzlo13/lightd/internal/hue/reconcile/group"
	"github.com/dokzlo13/lightd/internal/hue/reconcile/light"
	"github.com/dokzlo13/lightd/internal/storage"
)

// StoreRegistry provides centralized access to all typed stores.
// This replaces passing individual stores throughout the codebase.
type StoreRegistry struct {
	base       *storage.Store
	groupStore *storage.TypedStore[group.Desired]
	lightStore *storage.TypedStore[light.Desired]
}

// NewStoreRegistry creates a new store registry with typed stores for each resource kind.
func NewStoreRegistry(base *storage.Store) *StoreRegistry {
	return &StoreRegistry{
		base:       base,
		groupStore: storage.NewTypedStore[group.Desired](base, string(reconcile.KindGroup)),
		lightStore: storage.NewTypedStore[light.Desired](base, string(reconcile.KindLight)),
	}
}

// Groups returns the typed store for group desired state.
func (r *StoreRegistry) Groups() *storage.TypedStore[group.Desired] {
	return r.groupStore
}

// Lights returns the typed store for light desired state.
func (r *StoreRegistry) Lights() *storage.TypedStore[light.Desired] {
	return r.lightStore
}

// Clear removes all state from all stores.
func (r *StoreRegistry) Clear() error {
	if err := r.groupStore.Clear(); err != nil {
		return err
	}
	return r.lightStore.Clear()
}

