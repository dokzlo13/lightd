package light

import (
	"context"
	"strconv"

	"github.com/amimof/huego"
	"github.com/rs/zerolog/log"
)

// Applier applies desired state to lights.
type Applier interface {
	Apply(ctx context.Context, lightID string, desired Desired) error
	TurnOn(ctx context.Context, lightID string) error
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

// Apply applies the desired state to a light.
func (a *HueApplier) Apply(ctx context.Context, lightID string, desired Desired) error {
	id, err := strconv.Atoi(lightID)
	if err != nil {
		return err
	}

	light, err := a.bridge.GetLight(id)
	if err != nil {
		return err
	}

	// Build state to apply
	state := huego.State{}
	hasChanges := false

	if desired.Power != nil {
		state.On = *desired.Power
		hasChanges = true
	}

	if desired.Bri != nil {
		state.Bri = *desired.Bri
		hasChanges = true
	}

	if desired.Hue != nil {
		state.Hue = *desired.Hue
		hasChanges = true
	}

	if desired.Sat != nil {
		state.Sat = *desired.Sat
		hasChanges = true
	}

	if desired.Xy != nil {
		state.Xy = desired.Xy
		hasChanges = true
	}

	if desired.Ct != nil {
		state.Ct = *desired.Ct
		hasChanges = true
	}

	if hasChanges {
		log.Info().
			Str("light", lightID).
			Interface("state", state).
			Msg("Applying state to light")
		return light.SetState(state)
	}

	return nil
}

// TurnOn turns on a light.
func (a *HueApplier) TurnOn(ctx context.Context, lightID string) error {
	id, err := strconv.Atoi(lightID)
	if err != nil {
		return err
	}

	light, err := a.bridge.GetLight(id)
	if err != nil {
		return err
	}

	log.Info().Str("light", lightID).Msg("Turning on light")
	return light.On()
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
	return light.Off()
}

