package light

import (
	"github.com/dokzlo13/lightd/internal/hue/reconcile/compare"
)

// PowerState represents the power state of a light.
type PowerState int

const (
	PowerStateOff PowerState = iota
	PowerStateOn
)

// Action represents what reconciliation action needs to be taken.
type Action int

const (
	ActionNone Action = iota
	ActionTurnOnWithState
	ActionTurnOff
	ActionApplyState
)

// String returns a human-readable name for the action.
func (a Action) String() string {
	switch a {
	case ActionNone:
		return "none"
	case ActionTurnOnWithState:
		return "turn_on_with_state"
	case ActionTurnOff:
		return "turn_off"
	case ActionApplyState:
		return "apply_state"
	default:
		return "unknown"
	}
}

// DetermineAction determines what action to take based on desired and actual state.
// This is the core FSM logic for light reconciliation.
//
// Decision tree:
//  1. If light is OFF and we want ON → TurnOnWithState
//  2. If light is ON and we want OFF → TurnOff
//  3. If light is ON and state differs → ApplyState
//  4. Otherwise → None
func DetermineAction(desired Desired, actual Actual) Action {
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
	if actual.On != nil && *actual.On {
		return PowerStateOn
	}
	return PowerStateOff
}

// determineActionFromOff determines action when light is currently off.
func determineActionFromOff(desired Desired) Action {
	if !wantsPowerOn(desired) {
		return ActionNone
	}

	// Light is off and we want it on
	if hasColorProperties(desired) {
		return ActionTurnOnWithState
	}

	// Just turn on without specific state - still need to send On=true
	if desired.On != nil && *desired.On {
		return ActionTurnOnWithState
	}

	return ActionNone
}

// determineActionFromOn determines action when light is currently on.
func determineActionFromOn(desired Desired, actual Actual) Action {
	// First priority: power off
	if wantsPowerOff(desired) {
		return ActionTurnOff
	}

	// Second priority: color/brightness changes
	if hasColorProperties(desired) {
		if !stateMatches(desired, actual.State) {
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
	return s.Bri != nil || s.Hue != nil || s.Sat != nil ||
		len(s.Xy) >= 2 || s.Ct != nil
}

// stateMatches compares desired state against actual state.
// Only compares fields that are set in desired (nil = don't care).
// Uses tolerances for numeric values to account for bridge rounding.
func stateMatches(desired, actual State) bool {
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
