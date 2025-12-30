package group

import (
	"context"
	"strconv"
	"sync"
	"time"

	"github.com/amimof/huego"
)

// sceneRecord tracks what scene we applied to a group.
type sceneRecord struct {
	SceneName string
	AppliedAt time.Time
}

// ActualProvider provides actual state for groups with scene tracking.
// Always fetches from the bridge - no caching, as the bridge is the source of truth.
type ActualProvider struct {
	bridge *huego.Bridge

	mu               sync.RWMutex
	lastAppliedScene map[string]sceneRecord // groupID -> what we applied
}

// NewActualProvider creates a new actual state provider.
func NewActualProvider(bridge *huego.Bridge) *ActualProvider {
	return &ActualProvider{
		bridge:           bridge,
		lastAppliedScene: make(map[string]sceneRecord),
	}
}

// Get returns the actual state for a group by fetching from the bridge.
func (p *ActualProvider) Get(ctx context.Context, groupID string) (Actual, error) {
	state, err := p.fetchGroupState(groupID)
	if err != nil {
		return Actual{}, err
	}

	// Combine with our scene tracking
	p.mu.RLock()
	record := p.lastAppliedScene[groupID]
	p.mu.RUnlock()

	return Actual{
		AnyOn:            state.AnyOn,
		AllOn:            state.AllOn,
		LastAppliedScene: record.SceneName,
		AppliedAt:        record.AppliedAt,
	}, nil
}

// fetchGroupState fetches group state directly from the bridge.
func (p *ActualProvider) fetchGroupState(groupID string) (*huego.GroupState, error) {
	id, err := strconv.Atoi(groupID)
	if err != nil {
		return nil, err
	}

	group, err := p.bridge.GetGroup(id)
	if err != nil {
		return nil, err
	}

	if group.GroupState != nil {
		return group.GroupState, nil
	}

	// Return empty state if nil
	return &huego.GroupState{}, nil
}

// Update records the state we just applied (for scene tracking only).
// This doesn't cache power state - that's always fetched fresh from bridge.
func (p *ActualProvider) Update(groupID string, actual Actual) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if actual.LastAppliedScene != "" {
		p.lastAppliedScene[groupID] = sceneRecord{
			SceneName: actual.LastAppliedScene,
			AppliedAt: actual.AppliedAt,
		}
	} else {
		// Clear scene tracking when turning off
		delete(p.lastAppliedScene, groupID)
	}
}


