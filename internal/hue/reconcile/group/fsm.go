package group

// State represents the power state of a group.
type State int

const (
	StateOff State = iota
	StateOn
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
func DetermineAction(desired Desired, actual Actual) Action {
	currentState := deriveState(actual)

	switch currentState {
	case StateOff:
		return determineActionFromOff(desired)
	case StateOn:
		return determineActionFromOn(desired)
	}

	return ActionNone
}

// deriveState determines the current power state from actual.
func deriveState(actual Actual) State {
	if actual.AnyOn {
		return StateOn
	}
	return StateOff
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
	if hasColorProperties(desired) {
		return ActionTurnOnWithState
	}

	// Power on requested but no scene or state to apply
	// This shouldn't happen in normal usage, but we can't turn on without something
	return ActionNone
}

// determineActionFromOn determines action when group is currently on.
func determineActionFromOn(desired Desired) Action {
	// First priority: power off
	if wantsPowerOff(desired) {
		return ActionTurnOff
	}

	// Second priority: apply scene if one is desired
	// Always apply - we don't cache what scene is active, bridge is source of truth
	if desired.SceneName != "" {
		return ActionApplyScene
	}

	// Third priority: color/brightness changes (only if no scene is active)
	if hasColorProperties(desired) {
		return ActionApplyState
	}

	return ActionNone
}

// wantsPowerOn returns true if desired explicitly wants power on.
func wantsPowerOn(desired Desired) bool {
	return desired.Power != nil && *desired.Power
}

// wantsPowerOff returns true if desired explicitly wants power off.
func wantsPowerOff(desired Desired) bool {
	return desired.Power != nil && !*desired.Power
}

// hasColorProperties returns true if any color/brightness property is set.
func hasColorProperties(desired Desired) bool {
	return desired.Bri != nil || desired.Hue != nil || desired.Sat != nil ||
		desired.Xy != nil || desired.Ct != nil
}
