package group

import (
	"context"
	"strconv"

	"github.com/amimof/huego"
	"github.com/rs/zerolog/log"

	"github.com/dokzlo13/lightd/internal/cache"
)

// Applier applies scenes and states to Hue groups.
type Applier interface {
	TurnOnWithScene(ctx context.Context, groupID, sceneName string) error
	ApplyScene(ctx context.Context, groupID, sceneName string) error
	ApplyState(ctx context.Context, groupID string, desired Desired) error
	TurnOff(ctx context.Context, groupID string) error
}

// HueApplier implements Applier using the Hue bridge.
type HueApplier struct {
	bridge     *huego.Bridge
	sceneIndex *cache.SceneIndex
}

// NewHueApplier creates a new group applier.
func NewHueApplier(bridge *huego.Bridge, sceneIndex *cache.SceneIndex) *HueApplier {
	return &HueApplier{
		bridge:     bridge,
		sceneIndex: sceneIndex,
	}
}

// TurnOnWithScene turns on a group by activating a scene.
func (a *HueApplier) TurnOnWithScene(ctx context.Context, groupID, sceneName string) error {
	log.Info().
		Str("group", groupID).
		Str("scene", sceneName).
		Msg("Turning on with scene")

	scene, err := a.sceneIndex.FindByName(sceneName, groupID)
	if err != nil {
		return err
	}

	id, err := strconv.Atoi(groupID)
	if err != nil {
		return err
	}

	group, err := a.bridge.GetGroup(id)
	if err != nil {
		return err
	}

	return group.Scene(scene.ID)
}

// ApplyScene applies a scene to an already-on group.
func (a *HueApplier) ApplyScene(ctx context.Context, groupID, sceneName string) error {
	log.Info().
		Str("group", groupID).
		Str("scene", sceneName).
		Msg("Applying scene")

	scene, err := a.sceneIndex.FindByName(sceneName, groupID)
	if err != nil {
		return err
	}

	id, err := strconv.Atoi(groupID)
	if err != nil {
		return err
	}

	group, err := a.bridge.GetGroup(id)
	if err != nil {
		return err
	}

	return group.Scene(scene.ID)
}

// ApplyState applies color/brightness state to a group.
// If desired.Power is set, it will also turn the group on/off.
func (a *HueApplier) ApplyState(ctx context.Context, groupID string, desired Desired) error {
	id, err := strconv.Atoi(groupID)
	if err != nil {
		return err
	}

	group, err := a.bridge.GetGroup(id)
	if err != nil {
		return err
	}

	// Build state to apply
	state := huego.State{}
	hasChanges := false

	// Handle power state - this is critical for turning on
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
			Str("group", groupID).
			Interface("state", state).
			Msg("Applying state to group")
		return group.SetState(state)
	}

	return nil
}

// TurnOff turns off a group.
func (a *HueApplier) TurnOff(ctx context.Context, groupID string) error {
	log.Info().
		Str("group", groupID).
		Msg("Turning off")

	id, err := strconv.Atoi(groupID)
	if err != nil {
		return err
	}

	group, err := a.bridge.GetGroup(id)
	if err != nil {
		return err
	}

	return group.Off()
}
