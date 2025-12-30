package light

import (
	"context"

	"github.com/dokzlo13/lightd/internal/reconcile"
	"github.com/dokzlo13/lightd/internal/state"
)

// Provider provides light resources for reconciliation.
type Provider struct {
	store   *state.TypedStore[Desired]
	actual  *ActualProvider
	applier Applier
}

// NewProvider creates a new light provider.
func NewProvider(
	store *state.TypedStore[Desired],
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
	return reconcile.KindLight
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

// Store returns the typed store for direct access.
func (p *Provider) Store() *state.TypedStore[Desired] {
	return p.store
}

// ActualProvider returns the actual state provider.
func (p *Provider) ActualProvider() *ActualProvider {
	return p.actual
}

