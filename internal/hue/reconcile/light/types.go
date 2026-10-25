// Package light provides the reconciliation resource for individual Hue lights.
package light

import (
	"github.com/dokzlo13/lightd/internal/hue/bridge"
)

// State represents light state, aligned with Hue API structure.
// Uses pointers to distinguish "not set" from "set to zero/false".
// Works for both Actual (fully populated) and Desired (sparse).
type State = bridge.State

// Desired is the desired state for a light.
// Stored as JSON in the resource_state table.
// For lights, this is just State (no scenes like groups).
type Desired = State

// Actual is the actual state of a light (from Hue bridge).
// Contains the full color state plus reachability info.
type Actual struct {
	State
	Reachable bool `json:"reachable"`
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
