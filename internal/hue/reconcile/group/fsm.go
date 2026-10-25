package group

import (
	"github.com/dokzlo13/lightd/internal/hue/reconcile/compare"
	"math"
)

// PowerState represents the power state of a group.
type PowerState int

const (
	PowerStateOff PowerState = iota
	PowerStateOn
)

// Action represents what reconciliation action needs to be taken.
type Action int

const (
	ActionNone Action = iota
	ActionTurnOnWithScene
	ActionTurnOnWithState
	ActionTurnOff
	ActionApplyScene
	ActionApplyState
)

// String returns a human-readable name for the action.
func (a Action) String() string {
	switch a {
	case ActionNone:
		return "none"
	case ActionTurnOnWithScene:
		return "turn_on_with_scene"
	case ActionTurnOnWithState:
		return "turn_on_with_state"
	case ActionTurnOff:
		return "turn_off"
	case ActionApplyScene:
		return "apply_scene"
	case ActionApplyState:
		return "apply_state"
	default:
		return "unknown"
	}
}

// DetermineAction determines what action to take based on desired and actual state.
// This is the core FSM logic for group reconciliation.
//
// Decision tree:
//  1. If group is OFF and we want ON → TurnOnWithScene or TurnOnWithState
//  2. If group is ON and we want OFF → TurnOff
//  3. If group is ON and we want a scene → ApplyScene
//  4. If group is ON and state differs → ApplyState
//  5. Otherwise → None
func DetermineAction(desired Desired, actual Actual) Action {
	if desired.PowerMode == "next_on" {
		return ActionNone
	}
	currentPower := derivePowerState(actual)

	switch currentPower {
	case PowerStateOff:
		return determineActionFromOff(desired)
	case PowerStateOn:
		return determineActionFromOn(desired, actual)
	}

	return ActionNone
}

// derivePowerState determines the current power state from actual.
func derivePowerState(actual Actual) PowerState {
	if actual.AnyOn {
		return PowerStateOn
	}
	return PowerStateOff
}

// determineActionFromOff determines action when group is currently off.
func determineActionFromOff(desired Desired) Action {
	if !wantsPowerOn(desired) {
		return ActionNone
	}

	// Group is off and we want it on
	if desired.SceneName != "" {
		return ActionTurnOnWithScene
	}
	if hasColorProperties(desired.State) {
		return ActionTurnOnWithState
	}

	return ActionTurnOnWithState
}

// determineActionFromOn determines action when group is currently on.
func determineActionFromOn(desired Desired, actual Actual) Action {
	// First priority: power off
	if wantsPowerOff(desired) {
		return ActionTurnOff
	}

	// Second priority: apply scene if one is desired
	// We always apply scenes because we can't verify the current scene
	// (bridge doesn't report active scene name, only resulting state)
	if desired.SceneName != "" {
		return ActionApplyScene
	}

	if wantsPowerOn(desired) && !actual.AllOn {
		return ActionTurnOnWithState
	}

	if desired.Brightness != nil || len(desired.Gradient) > 0 || desired.Effect != "" {
		return ActionApplyState
	}

	// Third priority: color/brightness changes
	if hasColorProperties(desired.State) {
		if !stateMatches(desired.State, actual.State) {
			return ActionApplyState
		}
	}

	return ActionNone
}

// wantsPowerOn returns true if desired explicitly wants power on.
func wantsPowerOn(desired Desired) bool {
	return desired.On != nil && *desired.On
}

// wantsPowerOff returns true if desired explicitly wants power off.
func wantsPowerOff(desired Desired) bool {
	return desired.On != nil && !*desired.On
}

// hasColorProperties returns true if any color/brightness property is set.
func hasColorProperties(s State) bool {
	return s.Brightness != nil || s.Effect != "" || s.Bri != nil || s.Hue != nil || s.Sat != nil ||
		len(s.Xy) >= 2 || s.Ct != nil
}

// stateMatches compares desired state against actual state.
// Only compares fields that are set in desired (nil = don't care).
// Uses tolerances for numeric values to account for bridge rounding.
func stateMatches(desired, actual State) bool {
	if desired.Brightness != nil && (actual.Brightness == nil || math.Abs(*desired.Brightness-*actual.Brightness) > 0.8) {
		return false
	}
	if desired.Effect != "" && desired.Effect != actual.Effect {
		return false
	}
	if desired.Ct != nil && actual.ColorMode != "" && actual.ColorMode != "ct" {
		return false
	}
	if len(desired.Xy) >= 2 && actual.ColorMode != "" && actual.ColorMode != "xy" {
		return false
	}
	// Brightness
	if desired.Bri != nil {
		if actual.Bri == nil || !compare.BriMatches(*desired.Bri, *actual.Bri) {
			return false
		}
	}

	// Color temperature
	if desired.Ct != nil {
		if actual.Ct == nil || !compare.CtMatches(*desired.Ct, *actual.Ct) {
			return false
		}
	}

	// Hue
	if desired.Hue != nil {
		if actual.Hue == nil || !compare.HueMatches(*desired.Hue, *actual.Hue) {
			return false
		}
	}

	// Saturation
	if desired.Sat != nil {
		if actual.Sat == nil || !compare.SatMatches(*desired.Sat, *actual.Sat) {
			return false
		}
	}

	// CIE XY color
	if len(desired.Xy) >= 2 {
		if !compare.XyMatches(desired.Xy, actual.Xy) {
			return false
		}
	}

	return true
}

// StateMatches is exported for use in manual control detection.
// Returns true if actual state matches all non-nil fields in desired state.
func StateMatches(desired, actual State) bool {
	return stateMatches(desired, actual)
}

// StateDiffers returns true if any specified field in desired differs from actual.
// This is the inverse of StateMatches.
func StateDiffers(desired, actual State) bool {
	return !stateMatches(desired, actual)
}
