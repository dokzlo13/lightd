package light

import (
	"context"
	"strconv"

	"github.com/amimof/huego"
	"github.com/rs/zerolog/log"
)

// Applier applies desired state to lights.
type Applier interface {
	ApplyState(ctx context.Context, lightID string, desired Desired) error
	TurnOff(ctx context.Context, lightID string) error
}

// HueApplier implements Applier using the Hue bridge.
type HueApplier struct {
	bridge *huego.Bridge
}

// NewHueApplier creates a new light applier.
func NewHueApplier(bridge *huego.Bridge) *HueApplier {
	return &HueApplier{
		bridge: bridge,
	}
}

// ApplyState applies the desired state to a light.
// Uses the State.ToHuegoState() conversion for clean API alignment.
func (a *HueApplier) ApplyState(ctx context.Context, lightID string, desired Desired) error {
	id, err := strconv.Atoi(lightID)
	if err != nil {
		return err
	}

	light, err := a.bridge.GetLight(id)
	if err != nil {
		return err
	}

	// Convert our State to huego.State
	state := desired.ToHuegoState()

	// Ensure On is set if we have color properties
	if hasColorProperties(desired) && desired.On == nil {
		state.On = true
	}

	log.Info().
		Str("light", lightID).
		Interface("state", state).
		Msg("Applying state to light")

	return light.SetStateContext(ctx, state)
}

// TurnOff turns off a light.
func (a *HueApplier) TurnOff(ctx context.Context, lightID string) error {
	id, err := strconv.Atoi(lightID)
	if err != nil {
		return err
	}

	light, err := a.bridge.GetLight(id)
	if err != nil {
		return err
	}

	log.Info().Str("light", lightID).Msg("Turning off light")
	return light.OffContext(ctx)
}
