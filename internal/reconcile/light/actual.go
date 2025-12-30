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
		actual.On = light.State.On
		actual.Bri = light.State.Bri
		actual.Hue = light.State.Hue
		actual.Sat = light.State.Sat
		actual.Xy = light.State.Xy
		actual.Ct = light.State.Ct
	}

	return actual, nil
}


