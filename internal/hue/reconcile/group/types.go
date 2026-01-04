// Package group provides the reconciliation resource for Hue light groups.
package group

import (
	"github.com/amimof/huego"
)

// State represents light/group state, aligned with Hue API structure.
// Uses pointers to distinguish "not set" from "set to zero/false".
// Works for both Actual (fully populated) and Desired (sparse).
type State struct {
	On             *bool     `json:"on,omitempty"`
	Bri            *uint8    `json:"bri,omitempty"`
	Hue            *uint16   `json:"hue,omitempty"`
	Sat            *uint8    `json:"sat,omitempty"`
	Xy             []float32 `json:"xy,omitempty"`
	Ct             *uint16   `json:"ct,omitempty"`
	Alert          string    `json:"alert,omitempty"`
	Effect         string    `json:"effect,omitempty"`
	TransitionTime *uint16   `json:"transitiontime,omitempty"`
	ColorMode      string    `json:"colormode,omitempty"`
}

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

// ToHuegoState converts State to huego.State for API calls.
// Only includes non-nil fields.
func (s *State) ToHuegoState() huego.State {
	hs := huego.State{}

	if s.On != nil {
		hs.On = *s.On
	}
	if s.Bri != nil {
		hs.Bri = *s.Bri
	}
	if s.Hue != nil {
		hs.Hue = *s.Hue
	}
	if s.Sat != nil {
		hs.Sat = *s.Sat
	}
	if len(s.Xy) >= 2 {
		hs.Xy = s.Xy
	}
	if s.Ct != nil {
		hs.Ct = *s.Ct
	}
	if s.Alert != "" {
		hs.Alert = s.Alert
	}
	if s.Effect != "" {
		hs.Effect = s.Effect
	}
	if s.TransitionTime != nil {
		hs.TransitionTime = *s.TransitionTime
	}

	return hs
}

// FromHuegoState creates a State from huego.State.
// All fields are populated (actual state from bridge).
func FromHuegoState(hs *huego.State) State {
	if hs == nil {
		return State{}
	}

	s := State{
		On:        &hs.On,
		Bri:       &hs.Bri,
		Hue:       &hs.Hue,
		Sat:       &hs.Sat,
		ColorMode: hs.ColorMode,
	}

	if hs.Ct != 0 {
		s.Ct = &hs.Ct
	}
	if len(hs.Xy) >= 2 {
		s.Xy = hs.Xy
	}

	return s
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
