package hue

import (
	"sync"
	"time"

	"github.com/amimof/huego"
	"github.com/rs/zerolog/log"
)

// CachedGroup holds cached group state with timestamp.
type CachedGroup struct {
	State     huego.GroupState
	FetchedAt time.Time
}

// GroupCache is a pure cache for group state.
// It does NOT fetch from network - callers must do that.
// This follows the Single Responsibility Principle.
type GroupCache struct {
	mu     sync.RWMutex
	groups map[string]*CachedGroup
	ttl    time.Duration
}

// NewGroupCache creates a new group cache.
// Parameters:
//   - ttl: Time-to-live for cache entries (0 = use default 5 minutes)
func NewGroupCache(ttl time.Duration) *GroupCache {
	if ttl == 0 {
		ttl = 5 * time.Minute
	}

	log.Info().Dur("ttl", ttl).Msg("Group cache initialized")

	return &GroupCache{
		groups: make(map[string]*CachedGroup),
		ttl:    ttl,
	}
}

// Get returns cached group state, or nil if not cached or stale.
func (c *GroupCache) Get(id string) *huego.GroupState {
	c.mu.RLock()
	defer c.mu.RUnlock()

	cached, ok := c.groups[id]
	if !ok {
		return nil
	}

	// Check if stale
	if time.Since(cached.FetchedAt) > c.ttl {
		return nil
	}

	return &cached.State
}

// Set stores group state in the cache.
func (c *GroupCache) Set(id string, state huego.GroupState) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.groups[id] = &CachedGroup{
		State:     state,
		FetchedAt: time.Now(),
	}
}

// IsStale returns true if the cache entry is older than TTL or doesn't exist.
func (c *GroupCache) IsStale(id string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	cached, ok := c.groups[id]
	if !ok {
		return true
	}
	return time.Since(cached.FetchedAt) > c.ttl
}

// Invalidate removes an entry from the cache.
func (c *GroupCache) Invalidate(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.groups, id)
}

// Clear removes all entries from the cache.
func (c *GroupCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.groups = make(map[string]*CachedGroup)
}
