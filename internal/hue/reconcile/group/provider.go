package group

import (
	"context"

	"github.com/dokzlo13/lightd/internal/hue/reconcile"
	"github.com/dokzlo13/lightd/internal/storage"
)

// Provider provides group resources for reconciliation.
type Provider struct {
	store   *storage.TypedStore[Desired]
	actual  *ActualProvider
	applier Applier
}

// NewProvider creates a new group provider.
func NewProvider(
	store *storage.TypedStore[Desired],
	actual *ActualProvider,
	applier Applier,
) *Provider {
	return &Provider{
		store:   store,
		actual:  actual,
		applier: applier,
	}
}

// Kind returns the resource kind.
func (p *Provider) Kind() reconcile.Kind {
	return reconcile.KindGroup
}

// ListDirty returns resources that have changed since last reconcile.
func (p *Provider) ListDirty(ctx context.Context, lastVersions map[string]int64) ([]reconcile.Resource, error) {
	ids, err := p.store.GetDirty(lastVersions)
	if err != nil {
		return nil, err
	}

	resources := make([]reconcile.Resource, 0, len(ids))
	for _, id := range ids {
		resources = append(resources, NewResource(id, p.store, p.actual, p.applier))
	}

	return resources, nil
}

// Get returns a specific resource by ID.
func (p *Provider) Get(ctx context.Context, id string) (reconcile.Resource, error) {
	return NewResource(id, p.store, p.actual, p.applier), nil
}

// ListAllIDs returns all resource IDs that have desired state.
func (p *Provider) ListAllIDs(ctx context.Context) ([]string, error) {
	_, versions, err := p.store.GetAll()
	if err != nil {
		return nil, err
	}

	ids := make([]string, 0, len(versions))
	for id := range versions {
		ids = append(ids, id)
	}
	return ids, nil
}

// ClearCaches is a no-op for group provider.
// We don't cache state - the bridge is the source of truth.
func (p *Provider) ClearCaches() {}

// Store returns the typed store for direct access (e.g., from actions).
func (p *Provider) Store() *storage.TypedStore[Desired] {
	return p.store
}

// ActualProvider returns the actual state provider.
func (p *Provider) ActualProvider() *ActualProvider {
	return p.actual
}
