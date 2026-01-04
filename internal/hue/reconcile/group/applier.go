package group

import (
	"context"
	"strconv"

	"github.com/amimof/huego"
	"github.com/rs/zerolog/log"
)

// SceneFinder looks up a scene by name and group.
// This interface breaks the import cycle between hue and hue/reconcile/group.
type SceneFinder interface {
	FindByName(sceneName, groupID string) (*huego.Scene, error)
}

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
	sceneIndex SceneFinder
}

// NewHueApplier creates a new group applier.
func NewHueApplier(bridge *huego.Bridge, sceneIndex SceneFinder) *HueApplier {
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

	// Use scene activation which turns on the lights
	return group.SceneContext(ctx, scene.ID)
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

	return group.SceneContext(ctx, scene.ID)
}

// ApplyState applies color/brightness state to a group.
// Uses the State.ToHuegoState() conversion for clean API alignment.
func (a *HueApplier) ApplyState(ctx context.Context, groupID string, desired Desired) error {
	id, err := strconv.Atoi(groupID)
	if err != nil {
		return err
	}

	group, err := a.bridge.GetGroup(id)
	if err != nil {
		return err
	}

	// Convert our State to huego.State
	state := desired.State.ToHuegoState()

	// If On is explicitly set, include it
	if desired.On != nil {
		state.On = *desired.On
	}

	log.Info().
		Str("group", groupID).
		Interface("state", state).
		Msg("Applying state to group")

	return group.SetStateContext(ctx, state)
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

	return group.OffContext(ctx)
}
