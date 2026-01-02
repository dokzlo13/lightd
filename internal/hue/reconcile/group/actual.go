package group

import (
	"context"
	"strconv"

	"github.com/amimof/huego"
)

// ActualProvider provides actual state for groups.
// Always fetches from the bridge - the bridge is the source of truth.
type ActualProvider struct {
	bridge *huego.Bridge
}

// NewActualProvider creates a new actual state provider.
func NewActualProvider(bridge *huego.Bridge) *ActualProvider {
	return &ActualProvider{
		bridge: bridge,
	}
}

// Get returns the actual state for a group by fetching from the bridge.
func (p *ActualProvider) Get(ctx context.Context, groupID string) (Actual, error) {
	state, err := p.fetchGroupState(groupID)
	if err != nil {
		return Actual{}, err
	}

	return Actual{
		AnyOn: state.AnyOn,
		AllOn: state.AllOn,
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
