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
	id, err := strconv.Atoi(groupID)
	if err != nil {
		return Actual{}, err
	}

	group, err := p.bridge.GetGroup(id)
	if err != nil {
		return Actual{}, err
	}

	actual := Actual{}

	// Power state from GroupState
	if group.GroupState != nil {
		actual.AnyOn = group.GroupState.AnyOn
		actual.AllOn = group.GroupState.AllOn
	}

	// Color/brightness state from State (the "action" field in API)
	if group.State != nil {
		actual.State = FromHuegoState(group.State)
	}

	return actual, nil
}

// GetGroup fetches the full huego.Group object for direct manipulation.
// This is useful when we need to call methods on the group (SetStateContext, etc.)
func (p *ActualProvider) GetGroup(ctx context.Context, groupID string) (*huego.Group, error) {
	id, err := strconv.Atoi(groupID)
	if err != nil {
		return nil, err
	}

	return p.bridge.GetGroup(id)
}
