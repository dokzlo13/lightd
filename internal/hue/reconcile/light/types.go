// Package light provides the reconciliation resource for individual Hue lights.
package light

import (
	"github.com/amimof/huego"
)

// State represents light state, aligned with Hue API structure.
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
