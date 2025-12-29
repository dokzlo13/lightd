package app

import (
	"context"

	"github.com/dokzlo13/lightd/internal/hue"
)

// ActualStateAdapter wraps a Hue client and cache to implement ActualState interface.
// It provides cached access to group state, fetching from the bridge when cache is stale.
type ActualStateAdapter struct {
	client *hue.Client
	cache  *hue.GroupCache
}

// NewActualStateAdapter creates a new adapter.
func NewActualStateAdapter(client *hue.Client, cache *hue.GroupCache) *ActualStateAdapter {
	return &ActualStateAdapter{
		client: client,
		cache:  cache,
	}
}

// Group returns group state, using cache when available.
// Fetches from bridge if cache is stale or missing, then caches the result.
func (a *ActualStateAdapter) Group(ctx context.Context, id string) (*hue.GroupState, error) {
	// Check cache first
	if state := a.cache.Get(id); state != nil {
		return state, nil
	}

	// Cache miss or stale - fetch from bridge
	return a.FetchAndCache(ctx, id)
}

// FetchAndCache fetches fresh group state from bridge and updates cache.
// Use this when you need guaranteed fresh data (e.g., for toggle decisions).
func (a *ActualStateAdapter) FetchAndCache(ctx context.Context, id string) (*hue.GroupState, error) {
	group, err := a.client.GetGroup(ctx, id)
	if err != nil {
		return nil, err
	}

	a.cache.Set(id, group.State)
	return &group.State, nil
}

// UpdateGroupState updates just the state portion of a cached group.
// Used by reconciler to update cache after making changes.
func (a *ActualStateAdapter) UpdateGroupState(id string, state hue.GroupState) {
	a.cache.Set(id, state)
}
