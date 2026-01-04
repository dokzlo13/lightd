package light

import (
	"context"
	"strconv"

	"github.com/amimof/huego"
)

// ActualProvider provides actual state for lights.
// Always fetches from the bridge - no caching, as the bridge is the source of truth.
type ActualProvider struct {
	bridge *huego.Bridge
}

// NewActualProvider creates a new actual state provider.
func NewActualProvider(bridge *huego.Bridge) *ActualProvider {
	return &ActualProvider{
		bridge: bridge,
	}
}

// Get returns the actual state for a light by fetching from the bridge.
func (p *ActualProvider) Get(ctx context.Context, lightID string) (Actual, error) {
	id, err := strconv.Atoi(lightID)
	if err != nil {
		return Actual{}, err
	}

	light, err := p.bridge.GetLight(id)
	if err != nil {
		return Actual{}, err
	}

	actual := Actual{}

	if light.State != nil {
		actual.State = FromHuegoState(light.State)
		actual.Reachable = light.State.Reachable
	}

	return actual, nil
}

// GetLight fetches the full huego.Light object for direct manipulation.
// This is useful when we need to call methods on the light.
func (p *ActualProvider) GetLight(ctx context.Context, lightID string) (*huego.Light, error) {
	id, err := strconv.Atoi(lightID)
	if err != nil {
		return nil, err
	}

	return p.bridge.GetLight(id)
}
