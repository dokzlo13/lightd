// Package group provides the reconciliation resource for Hue light groups.
package group

import (
	"github.com/dokzlo13/lightd/internal/hue/bridge"
)

// State represents light/group state, aligned with Hue API structure.
// Uses pointers to distinguish "not set" from "set to zero/false".
// Works for both Actual (fully populated) and Desired (sparse).
type State = bridge.State

// Desired is the desired state for a group.
// Stored as JSON in the resource_state table.
// Extends State with SceneName for scene-based control.
type Desired struct {
	State
	SceneName string `json:"scene_name,omitempty"` // Scene name to apply (resolved to ID at apply time)
}

// Actual is the actual state of a group (from Hue bridge).
// Contains full color state plus aggregate power state.
type Actual struct {
	State
	AnyOn bool `json:"any_on"` // At least one light is on
	AllOn bool `json:"all_on"` // All lights are on
}

// Helper functions for creating pointers

func BoolPtr(b bool) *bool {
	return &b
}

func Uint8Ptr(v uint8) *uint8 {
	return &v
}

func Uint16Ptr(v uint16) *uint16 {
	return &v
}
