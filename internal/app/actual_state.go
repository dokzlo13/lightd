package app

import (
	"context"
	"strconv"

	"github.com/amimof/huego"

	"github.com/dokzlo13/lightd/internal/hue"
)

// ActualStateAdapter wraps a huego bridge and cache to implement ActualState interface.
// It provides cached access to group state, fetching from the bridge when cache is stale.
type ActualStateAdapter struct {
	bridge *huego.Bridge
	cache  *hue.GroupCache
}

// NewActualStateAdapter creates a new adapter.
func NewActualStateAdapter(bridge *huego.Bridge, cache *hue.GroupCache) *ActualStateAdapter {
	return &ActualStateAdapter{
		bridge: bridge,
		cache:  cache,
	}
}

// Group returns group state, using cache when available.
// Fetches from bridge if cache is stale or missing, then caches the result.
func (a *ActualStateAdapter) Group(ctx context.Context, id string) (*huego.GroupState, error) {
	// Check cache first
	if state := a.cache.Get(id); state != nil {
		return state, nil
	}

	// Cache miss or stale - fetch from bridge
	return a.FetchAndCache(ctx, id)
}

// FetchAndCache fetches fresh group state from bridge and updates cache.
// Use this when you need guaranteed fresh data (e.g., for toggle decisions).
func (a *ActualStateAdapter) FetchAndCache(ctx context.Context, id string) (*huego.GroupState, error) {
	groupID, err := strconv.Atoi(id)
	if err != nil {
		return nil, err
	}

	group, err := a.bridge.GetGroup(groupID)
	if err != nil {
		return nil, err
	}

	if group.GroupState != nil {
		a.cache.Set(id, *group.GroupState)
		return group.GroupState, nil
	}

	// Return empty state if nil
	emptyState := huego.GroupState{}
	a.cache.Set(id, emptyState)
	return &emptyState, nil
}

// UpdateGroupState updates just the state portion of a cached group.
// Used by reconciler to update cache after making changes.
func (a *ActualStateAdapter) UpdateGroupState(id string, state huego.GroupState) {
	a.cache.Set(id, state)
}
