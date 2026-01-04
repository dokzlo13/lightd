package light

import (
	"testing"
)

func TestDetermineAction(t *testing.T) {
	tests := []struct {
		name     string
		desired  Desired
		actual   Actual
		expected Action
	}{
		// =========================================================================
		// LIGHT OFF CASES
		// =========================================================================
		{
			name:    "off/no_desired_state",
			desired: Desired{},
			actual: Actual{
				State: State{On: BoolPtr(false)},
			},
			expected: ActionNone,
		},
		{
			name: "off/wants_power_on_with_brightness",
			desired: Desired{
				On:  BoolPtr(true),
				Bri: Uint8Ptr(254),
			},
			actual: Actual{
				State: State{On: BoolPtr(false)},
			},
			expected: ActionTurnOnWithState,
		},
		{
			name: "off/wants_power_on_with_color_temp",
			desired: Desired{
				On: BoolPtr(true),
				Ct: Uint16Ptr(300),
			},
			actual: Actual{
				State: State{On: BoolPtr(false)},
			},
			expected: ActionTurnOnWithState,
		},
		{
			name: "off/wants_power_on_with_hue_sat",
			desired: Desired{
				On:  BoolPtr(true),
				Hue: Uint16Ptr(10000),
				Sat: Uint8Ptr(200),
			},
			actual: Actual{
				State: State{On: BoolPtr(false)},
			},
			expected: ActionTurnOnWithState,
		},
		{
			name: "off/wants_power_on_with_xy",
			desired: Desired{
				On: BoolPtr(true),
				Xy: []float32{0.5, 0.5},
			},
			actual: Actual{
				State: State{On: BoolPtr(false)},
			},
			expected: ActionTurnOnWithState,
		},
		{
			name: "off/wants_power_on_only",
			desired: Desired{
				On: BoolPtr(true),
			},
			actual: Actual{
				State: State{On: BoolPtr(false)},
			},
			expected: ActionTurnOnWithState,
		},
		{
			name: "off/wants_power_off",
			desired: Desired{
				On: BoolPtr(false),
			},
			actual: Actual{
				State: State{On: BoolPtr(false)},
			},
			expected: ActionNone, // Already off
		},
		{
			name: "off/brightness_without_power_request",
			desired: Desired{
				Bri: Uint8Ptr(200),
			},
			actual: Actual{
				State: State{On: BoolPtr(false)},
			},
			expected: ActionNone, // No explicit power on request
		},

		// =========================================================================
		// LIGHT ON CASES - POWER CONTROL
		// =========================================================================
		{
			name:    "on/no_desired_state",
			desired: Desired{},
			actual: Actual{
				State: State{On: BoolPtr(true)},
			},
			expected: ActionNone,
		},
		{
			name: "on/wants_power_off",
			desired: Desired{
				On: BoolPtr(false),
			},
			actual: Actual{
				State: State{On: BoolPtr(true)},
			},
			expected: ActionTurnOff,
		},
		{
			name: "on/wants_power_off_with_brightness",
			desired: Desired{
				On:  BoolPtr(false),
				Bri: Uint8Ptr(200), // Should be ignored
			},
			actual: Actual{
				State: State{On: BoolPtr(true)},
			},
			expected: ActionTurnOff, // Power off takes priority
		},
		{
			name: "on/wants_power_on_already_on",
			desired: Desired{
				On: BoolPtr(true),
			},
			actual: Actual{
				State: State{On: BoolPtr(true)},
			},
			expected: ActionNone, // Already on, no color to apply
		},

		// =========================================================================
		// LIGHT ON CASES - STATE APPLICATION
		// =========================================================================
		{
			name: "on/apply_brightness_when_different",
			desired: Desired{
				Bri: Uint8Ptr(200),
			},
			actual: Actual{
				State: State{On: BoolPtr(true), Bri: Uint8Ptr(100)},
			},
			expected: ActionApplyState,
		},
		{
			name: "on/apply_color_temp_when_different",
			desired: Desired{
				Ct: Uint16Ptr(300),
			},
			actual: Actual{
				State: State{On: BoolPtr(true), Ct: Uint16Ptr(450)},
			},
			expected: ActionApplyState,
		},
		{
			name: "on/apply_hue_when_different",
			desired: Desired{
				Hue: Uint16Ptr(30000),
			},
			actual: Actual{
				State: State{On: BoolPtr(true), Hue: Uint16Ptr(10000)},
			},
			expected: ActionApplyState,
		},
		{
			name: "on/apply_saturation_when_different",
			desired: Desired{
				Sat: Uint8Ptr(254),
			},
			actual: Actual{
				State: State{On: BoolPtr(true), Sat: Uint8Ptr(100)},
			},
			expected: ActionApplyState,
		},
		{
			name: "on/apply_xy_when_different",
			desired: Desired{
				Xy: []float32{0.5, 0.5},
			},
			actual: Actual{
				State: State{On: BoolPtr(true), Xy: []float32{0.3, 0.3}},
			},
			expected: ActionApplyState,
		},

		// =========================================================================
		// LIGHT ON CASES - STATE MATCHES (NO ACTION NEEDED)
		// =========================================================================
		{
			name: "on/brightness_matches_exactly",
			desired: Desired{
				Bri: Uint8Ptr(200),
			},
			actual: Actual{
				State: State{On: BoolPtr(true), Bri: Uint8Ptr(200)},
			},
			expected: ActionNone,
		},
		{
			name: "on/brightness_matches_within_tolerance",
			desired: Desired{
				Bri: Uint8Ptr(200),
			},
			actual: Actual{
				State: State{On: BoolPtr(true), Bri: Uint8Ptr(201)}, // Within ±2 tolerance
			},
			expected: ActionNone,
		},
		{
			name: "on/ct_matches_within_tolerance",
			desired: Desired{
				Ct: Uint16Ptr(300),
			},
			actual: Actual{
				State: State{On: BoolPtr(true), Ct: Uint16Ptr(303)}, // Within ±5 tolerance
			},
			expected: ActionNone,
		},
		{
			name: "on/hue_matches_within_tolerance",
			desired: Desired{
				Hue: Uint16Ptr(30000),
			},
			actual: Actual{
				State: State{On: BoolPtr(true), Hue: Uint16Ptr(30050)}, // Within ±100 tolerance
			},
			expected: ActionNone,
		},
		{
			name: "on/xy_matches_within_tolerance",
			desired: Desired{
				Xy: []float32{0.5, 0.5},
			},
			actual: Actual{
				State: State{On: BoolPtr(true), Xy: []float32{0.505, 0.495}}, // Within ±0.01 tolerance
			},
			expected: ActionNone,
		},
		{
			name: "on/multiple_properties_all_match",
			desired: Desired{
				Bri: Uint8Ptr(200),
				Ct:  Uint16Ptr(300),
			},
			actual: Actual{
				State: State{
					On:  BoolPtr(true),
					Bri: Uint8Ptr(200),
					Ct:  Uint16Ptr(300),
				},
			},
			expected: ActionNone,
		},
		{
			name: "on/one_property_differs_triggers_apply",
			desired: Desired{
				Bri: Uint8Ptr(200),
				Ct:  Uint16Ptr(300),
			},
			actual: Actual{
				State: State{
					On:  BoolPtr(true),
					Bri: Uint8Ptr(200),
					Ct:  Uint16Ptr(450), // This differs
				},
			},
			expected: ActionApplyState,
		},

		// =========================================================================
		// EDGE CASES
		// =========================================================================
		{
			name: "edge/actual_has_nil_state_desired_has_bri",
			desired: Desired{
				Bri: Uint8Ptr(200),
			},
			actual: Actual{
				State: State{On: BoolPtr(true)}, // No brightness info
			},
			expected: ActionApplyState, // actual.Bri is nil, doesn't match
		},
		{
			name: "edge/actual_has_nil_xy_desired_has_xy",
			desired: Desired{
				Xy: []float32{0.5, 0.5},
			},
			actual: Actual{
				State: State{On: BoolPtr(true)}, // No XY
			},
			expected: ActionApplyState,
		},
		{
			name: "edge/desired_only_cares_about_ct_other_differs",
			desired: Desired{
				Ct: Uint16Ptr(300),
			},
			actual: Actual{
				State: State{
					On:  BoolPtr(true),
					Ct:  Uint16Ptr(300),  // Matches
					Bri: Uint8Ptr(50),    // Differs but not in desired
					Hue: Uint16Ptr(1000), // Differs but not in desired
				},
			},
			expected: ActionNone, // Only compare what's in desired
		},
		{
			name: "edge/light_unreachable_still_determines_action",
			desired: Desired{
				On:  BoolPtr(true),
				Bri: Uint8Ptr(200),
			},
			actual: Actual{
				State:     State{On: BoolPtr(false)},
				Reachable: false,
			},
			expected: ActionTurnOnWithState, // FSM doesn't care about reachability
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DetermineAction(tt.desired, tt.actual)
			if got != tt.expected {
				t.Errorf("DetermineAction() = %v (%s), want %v (%s)",
					got, got.String(), tt.expected, tt.expected.String())
			}
		})
	}
}

func TestDerivePowerState(t *testing.T) {
	tests := []struct {
		name     string
		actual   Actual
		expected PowerState
	}{
		{
			name: "off_explicit",
			actual: Actual{
				State: State{On: BoolPtr(false)},
			},
			expected: PowerStateOff,
		},
		{
			name: "on_explicit",
			actual: Actual{
				State: State{On: BoolPtr(true)},
			},
			expected: PowerStateOn,
		},
		{
			name: "nil_on_treated_as_off",
			actual: Actual{
				State: State{}, // On is nil
			},
			expected: PowerStateOff,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := derivePowerState(tt.actual)
			if got != tt.expected {
				t.Errorf("derivePowerState() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestWantsPowerOn(t *testing.T) {
	tests := []struct {
		name     string
		desired  Desired
		expected bool
	}{
		{
			name:     "nil_power",
			desired:  Desired{},
			expected: false,
		},
		{
			name:     "power_true",
			desired:  Desired{On: BoolPtr(true)},
			expected: true,
		},
		{
			name:     "power_false",
			desired:  Desired{On: BoolPtr(false)},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := wantsPowerOn(tt.desired)
			if got != tt.expected {
				t.Errorf("wantsPowerOn() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestWantsPowerOff(t *testing.T) {
	tests := []struct {
		name     string
		desired  Desired
		expected bool
	}{
		{
			name:     "nil_power",
			desired:  Desired{},
			expected: false,
		},
		{
			name:     "power_true",
			desired:  Desired{On: BoolPtr(true)},
			expected: false,
		},
		{
			name:     "power_false",
			desired:  Desired{On: BoolPtr(false)},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := wantsPowerOff(tt.desired)
			if got != tt.expected {
				t.Errorf("wantsPowerOff() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestHasColorProperties(t *testing.T) {
	tests := []struct {
		name     string
		state    State
		expected bool
	}{
		{
			name:     "empty",
			state:    State{},
			expected: false,
		},
		{
			name:     "only_power",
			state:    State{On: BoolPtr(true)},
			expected: false,
		},
		{
			name:     "has_bri",
			state:    State{Bri: Uint8Ptr(128)},
			expected: true,
		},
		{
			name:     "has_hue",
			state:    State{Hue: Uint16Ptr(10000)},
			expected: true,
		},
		{
			name:     "has_sat",
			state:    State{Sat: Uint8Ptr(200)},
			expected: true,
		},
		{
			name:     "has_xy",
			state:    State{Xy: []float32{0.5, 0.5}},
			expected: true,
		},
		{
			name:     "has_ct",
			state:    State{Ct: Uint16Ptr(300)},
			expected: true,
		},
		{
			name:     "has_multiple",
			state:    State{Bri: Uint8Ptr(200), Ct: Uint16Ptr(400)},
			expected: true,
		},
		{
			name:     "xy_empty_slice",
			state:    State{Xy: []float32{}},
			expected: false,
		},
		{
			name:     "xy_single_element",
			state:    State{Xy: []float32{0.5}},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hasColorProperties(tt.state)
			if got != tt.expected {
				t.Errorf("hasColorProperties() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestStateMatches(t *testing.T) {
	tests := []struct {
		name     string
		desired  State
		actual   State
		expected bool
	}{
		// Empty states
		{
			name:     "both_empty",
			desired:  State{},
			actual:   State{},
			expected: true,
		},
		{
			name:     "desired_empty_actual_has_values",
			desired:  State{},
			actual:   State{Bri: Uint8Ptr(200), Ct: Uint16Ptr(300)},
			expected: true, // Desired doesn't care
		},

		// Brightness tests
		{
			name:     "bri_exact_match",
			desired:  State{Bri: Uint8Ptr(200)},
			actual:   State{Bri: Uint8Ptr(200)},
			expected: true,
		},
		{
			name:     "bri_within_tolerance_plus_1",
			desired:  State{Bri: Uint8Ptr(200)},
			actual:   State{Bri: Uint8Ptr(201)},
			expected: true,
		},
		{
			name:     "bri_within_tolerance_minus_2",
			desired:  State{Bri: Uint8Ptr(200)},
			actual:   State{Bri: Uint8Ptr(198)},
			expected: true,
		},
		{
			name:     "bri_outside_tolerance",
			desired:  State{Bri: Uint8Ptr(200)},
			actual:   State{Bri: Uint8Ptr(196)},
			expected: false,
		},
		{
			name:     "bri_actual_nil",
			desired:  State{Bri: Uint8Ptr(200)},
			actual:   State{},
			expected: false,
		},

		// Color temperature tests
		{
			name:     "ct_exact_match",
			desired:  State{Ct: Uint16Ptr(300)},
			actual:   State{Ct: Uint16Ptr(300)},
			expected: true,
		},
		{
			name:     "ct_within_tolerance",
			desired:  State{Ct: Uint16Ptr(300)},
			actual:   State{Ct: Uint16Ptr(304)},
			expected: true,
		},
		{
			name:     "ct_outside_tolerance",
			desired:  State{Ct: Uint16Ptr(300)},
			actual:   State{Ct: Uint16Ptr(310)},
			expected: false,
		},

		// Hue tests
		{
			name:     "hue_exact_match",
			desired:  State{Hue: Uint16Ptr(30000)},
			actual:   State{Hue: Uint16Ptr(30000)},
			expected: true,
		},
		{
			name:     "hue_within_tolerance",
			desired:  State{Hue: Uint16Ptr(30000)},
			actual:   State{Hue: Uint16Ptr(30099)},
			expected: true,
		},
		{
			name:     "hue_outside_tolerance",
			desired:  State{Hue: Uint16Ptr(30000)},
			actual:   State{Hue: Uint16Ptr(30150)},
			expected: false,
		},

		// Saturation tests
		{
			name:     "sat_exact_match",
			desired:  State{Sat: Uint8Ptr(200)},
			actual:   State{Sat: Uint8Ptr(200)},
			expected: true,
		},
		{
			name:     "sat_within_tolerance",
			desired:  State{Sat: Uint8Ptr(200)},
			actual:   State{Sat: Uint8Ptr(202)},
			expected: true,
		},
		{
			name:     "sat_outside_tolerance",
			desired:  State{Sat: Uint8Ptr(200)},
			actual:   State{Sat: Uint8Ptr(205)},
			expected: false,
		},

		// XY tests
		{
			name:     "xy_exact_match",
			desired:  State{Xy: []float32{0.5, 0.4}},
			actual:   State{Xy: []float32{0.5, 0.4}},
			expected: true,
		},
		{
			name:     "xy_within_tolerance",
			desired:  State{Xy: []float32{0.5, 0.4}},
			actual:   State{Xy: []float32{0.505, 0.395}},
			expected: true,
		},
		{
			name:     "xy_x_outside_tolerance",
			desired:  State{Xy: []float32{0.5, 0.4}},
			actual:   State{Xy: []float32{0.52, 0.4}},
			expected: false,
		},
		{
			name:     "xy_y_outside_tolerance",
			desired:  State{Xy: []float32{0.5, 0.4}},
			actual:   State{Xy: []float32{0.5, 0.42}},
			expected: false,
		},
		{
			name:     "xy_actual_too_short",
			desired:  State{Xy: []float32{0.5, 0.4}},
			actual:   State{Xy: []float32{0.5}},
			expected: false,
		},
		{
			name:     "xy_actual_nil",
			desired:  State{Xy: []float32{0.5, 0.4}},
			actual:   State{},
			expected: false,
		},

		// Multiple properties
		{
			name:     "multiple_all_match",
			desired:  State{Bri: Uint8Ptr(200), Ct: Uint16Ptr(300)},
			actual:   State{Bri: Uint8Ptr(200), Ct: Uint16Ptr(300)},
			expected: true,
		},
		{
			name:     "multiple_one_fails",
			desired:  State{Bri: Uint8Ptr(200), Ct: Uint16Ptr(300)},
			actual:   State{Bri: Uint8Ptr(200), Ct: Uint16Ptr(400)},
			expected: false,
		},
		{
			name:     "actual_has_extra_fields",
			desired:  State{Bri: Uint8Ptr(200)},
			actual:   State{Bri: Uint8Ptr(200), Ct: Uint16Ptr(300), Hue: Uint16Ptr(40000)},
			expected: true, // Extra fields in actual are ignored
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := StateMatches(tt.desired, tt.actual)
			if got != tt.expected {
				t.Errorf("StateMatches() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestActionString(t *testing.T) {
	tests := []struct {
		action   Action
		expected string
	}{
		{ActionNone, "none"},
		{ActionTurnOnWithState, "turn_on_with_state"},
		{ActionTurnOff, "turn_off"},
		{ActionApplyState, "apply_state"},
		{Action(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			got := tt.action.String()
			if got != tt.expected {
				t.Errorf("Action.String() = %v, want %v", got, tt.expected)
			}
		})
	}
}

// Note: Tolerance helper tests are in the compare package.

func TestFromHuegoState(t *testing.T) {
	t.Run("nil_input", func(t *testing.T) {
		got := FromHuegoState(nil)
		if got.On != nil || got.Bri != nil || got.Ct != nil {
			t.Error("FromHuegoState(nil) should return empty State")
		}
	})
}

func TestToHuegoState(t *testing.T) {
	t.Run("only_set_fields_included", func(t *testing.T) {
		state := State{
			On:  BoolPtr(true),
			Bri: Uint8Ptr(200),
			// Ct is nil, should not be included
		}

		hs := state.ToHuegoState()

		if !hs.On {
			t.Error("On should be true")
		}
		if hs.Bri != 200 {
			t.Error("Bri should be 200")
		}
	})
}
