package group

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
		// GROUP OFF CASES
		// =========================================================================
		{
			name:     "off/no_desired_state",
			desired:  Desired{},
			actual:   Actual{AnyOn: false, AllOn: false},
			expected: ActionNone,
		},
		{
			name: "off/wants_power_on_with_scene",
			desired: Desired{
				State:     State{On: BoolPtr(true)},
				SceneName: "Relax",
			},
			actual:   Actual{AnyOn: false, AllOn: false},
			expected: ActionTurnOnWithScene,
		},
		{
			name: "off/wants_power_on_with_brightness",
			desired: Desired{
				State: State{On: BoolPtr(true), Bri: Uint8Ptr(254)},
			},
			actual:   Actual{AnyOn: false, AllOn: false},
			expected: ActionTurnOnWithState,
		},
		{
			name: "off/wants_power_on_with_color_temp",
			desired: Desired{
				State: State{On: BoolPtr(true), Ct: Uint16Ptr(300)},
			},
			actual:   Actual{AnyOn: false, AllOn: false},
			expected: ActionTurnOnWithState,
		},
		{
			name: "off/wants_power_on_with_hue_sat",
			desired: Desired{
				State: State{On: BoolPtr(true), Hue: Uint16Ptr(10000), Sat: Uint8Ptr(200)},
			},
			actual:   Actual{AnyOn: false, AllOn: false},
			expected: ActionTurnOnWithState,
		},
		{
			name: "off/wants_power_on_with_xy",
			desired: Desired{
				State: State{On: BoolPtr(true), Xy: []float32{0.5, 0.5}},
			},
			actual:   Actual{AnyOn: false, AllOn: false},
			expected: ActionTurnOnWithState,
		},
		{
			name: "off/wants_power_on_no_properties",
			desired: Desired{
				State: State{On: BoolPtr(true)},
			},
			actual:   Actual{AnyOn: false, AllOn: false},
			expected: ActionTurnOnWithState, // On-only commands are supported
		},
		{
			name: "off/wants_power_off",
			desired: Desired{
				State: State{On: BoolPtr(false)},
			},
			actual:   Actual{AnyOn: false, AllOn: false},
			expected: ActionNone, // Already off
		},
		{
			name: "off/scene_without_power_request",
			desired: Desired{
				SceneName: "Relax",
			},
			actual:   Actual{AnyOn: false, AllOn: false},
			expected: ActionNone, // No explicit power on request
		},

		// =========================================================================
		// GROUP ON CASES - POWER CONTROL
		// =========================================================================
		{
			name:     "on/no_desired_state",
			desired:  Desired{},
			actual:   Actual{AnyOn: true, AllOn: true},
			expected: ActionNone,
		},
		{
			name: "on/wants_power_off",
			desired: Desired{
				State: State{On: BoolPtr(false)},
			},
			actual:   Actual{AnyOn: true, AllOn: true},
			expected: ActionTurnOff,
		},
		{
			name: "on/wants_power_off_with_scene",
			desired: Desired{
				State:     State{On: BoolPtr(false)},
				SceneName: "Relax",
			},
			actual:   Actual{AnyOn: true, AllOn: true},
			expected: ActionTurnOff, // Power off takes priority over scene
		},
		{
			name: "on/wants_power_on_already_on",
			desired: Desired{
				State: State{On: BoolPtr(true)},
			},
			actual:   Actual{AnyOn: true, AllOn: true},
			expected: ActionNone, // Already on, no scene or color to apply
		},

		// =========================================================================
		// GROUP ON CASES - SCENE APPLICATION
		// =========================================================================
		{
			name: "on/apply_scene",
			desired: Desired{
				SceneName: "Relax",
			},
			actual:   Actual{AnyOn: true, AllOn: true},
			expected: ActionApplyScene,
		},
		{
			name: "on/apply_scene_with_power_on",
			desired: Desired{
				State:     State{On: BoolPtr(true)},
				SceneName: "Energize",
			},
			actual:   Actual{AnyOn: true, AllOn: true},
			expected: ActionApplyScene,
		},
		{
			name: "on/scene_takes_priority_over_color",
			desired: Desired{
				State:     State{Bri: Uint8Ptr(200)},
				SceneName: "Concentrate",
			},
			actual:   Actual{AnyOn: true, AllOn: true},
			expected: ActionApplyScene, // Scene has priority
		},

		// =========================================================================
		// GROUP ON CASES - STATE APPLICATION (NO SCENE)
		// =========================================================================
		{
			name: "on/apply_brightness_when_different",
			desired: Desired{
				State: State{Bri: Uint8Ptr(200)},
			},
			actual: Actual{
				AnyOn: true, AllOn: true,
				State: State{Bri: Uint8Ptr(100)},
			},
			expected: ActionApplyState,
		},
		{
			name: "on/apply_color_temp_when_different",
			desired: Desired{
				State: State{Ct: Uint16Ptr(300)},
			},
			actual: Actual{
				AnyOn: true, AllOn: true,
				State: State{Ct: Uint16Ptr(450)},
			},
			expected: ActionApplyState,
		},
		{
			name: "on/apply_hue_when_different",
			desired: Desired{
				State: State{Hue: Uint16Ptr(30000)},
			},
			actual: Actual{
				AnyOn: true, AllOn: true,
				State: State{Hue: Uint16Ptr(10000)},
			},
			expected: ActionApplyState,
		},
		{
			name: "on/apply_saturation_when_different",
			desired: Desired{
				State: State{Sat: Uint8Ptr(254)},
			},
			actual: Actual{
				AnyOn: true, AllOn: true,
				State: State{Sat: Uint8Ptr(100)},
			},
			expected: ActionApplyState,
		},
		{
			name: "on/apply_xy_when_different",
			desired: Desired{
				State: State{Xy: []float32{0.5, 0.5}},
			},
			actual: Actual{
				AnyOn: true, AllOn: true,
				State: State{Xy: []float32{0.3, 0.3}},
			},
			expected: ActionApplyState,
		},

		// =========================================================================
		// GROUP ON CASES - STATE MATCHES (NO ACTION NEEDED)
		// =========================================================================
		{
			name: "on/brightness_matches_exactly",
			desired: Desired{
				State: State{Bri: Uint8Ptr(200)},
			},
			actual: Actual{
				AnyOn: true, AllOn: true,
				State: State{Bri: Uint8Ptr(200)},
			},
			expected: ActionNone,
		},
		{
			name: "on/brightness_matches_within_tolerance",
			desired: Desired{
				State: State{Bri: Uint8Ptr(200)},
			},
			actual: Actual{
				AnyOn: true, AllOn: true,
				State: State{Bri: Uint8Ptr(201)}, // Within ±2 tolerance
			},
			expected: ActionNone,
		},
		{
			name: "on/ct_matches_within_tolerance",
			desired: Desired{
				State: State{Ct: Uint16Ptr(300)},
			},
			actual: Actual{
				AnyOn: true, AllOn: true,
				State: State{Ct: Uint16Ptr(303)}, // Within ±5 tolerance
			},
			expected: ActionNone,
		},
		{
			name: "on/hue_matches_within_tolerance",
			desired: Desired{
				State: State{Hue: Uint16Ptr(30000)},
			},
			actual: Actual{
				AnyOn: true, AllOn: true,
				State: State{Hue: Uint16Ptr(30050)}, // Within ±100 tolerance
			},
			expected: ActionNone,
		},
		{
			name: "on/xy_matches_within_tolerance",
			desired: Desired{
				State: State{Xy: []float32{0.5, 0.5}},
			},
			actual: Actual{
				AnyOn: true, AllOn: true,
				State: State{Xy: []float32{0.505, 0.495}}, // Within ±0.01 tolerance
			},
			expected: ActionNone,
		},
		{
			name: "on/multiple_properties_all_match",
			desired: Desired{
				State: State{
					Bri: Uint8Ptr(200),
					Ct:  Uint16Ptr(300),
				},
			},
			actual: Actual{
				AnyOn: true, AllOn: true,
				State: State{
					Bri: Uint8Ptr(200),
					Ct:  Uint16Ptr(300),
				},
			},
			expected: ActionNone,
		},
		{
			name: "on/one_property_differs_triggers_apply",
			desired: Desired{
				State: State{
					Bri: Uint8Ptr(200),
					Ct:  Uint16Ptr(300),
				},
			},
			actual: Actual{
				AnyOn: true, AllOn: true,
				State: State{
					Bri: Uint8Ptr(200),
					Ct:  Uint16Ptr(450), // This differs
				},
			},
			expected: ActionApplyState,
		},

		// =========================================================================
		// PARTIAL ON CASES (AnyOn=true, AllOn=false)
		// =========================================================================
		{
			name: "partial_on/apply_scene",
			desired: Desired{
				SceneName: "Relax",
			},
			actual:   Actual{AnyOn: true, AllOn: false},
			expected: ActionApplyScene,
		},
		{
			name: "partial_on/wants_power_off",
			desired: Desired{
				State: State{On: BoolPtr(false)},
			},
			actual:   Actual{AnyOn: true, AllOn: false},
			expected: ActionTurnOff,
		},
		{
			name: "partial_on/apply_state_when_different",
			desired: Desired{
				State: State{Bri: Uint8Ptr(254)},
			},
			actual: Actual{
				AnyOn: true, AllOn: false,
				State: State{Bri: Uint8Ptr(100)},
			},
			expected: ActionApplyState,
		},

		// =========================================================================
		// EDGE CASES
		// =========================================================================
		{
			name: "edge/actual_has_nil_state_desired_has_bri",
			desired: Desired{
				State: State{Bri: Uint8Ptr(200)},
			},
			actual: Actual{
				AnyOn: true, AllOn: true,
				State: State{}, // No brightness info
			},
			expected: ActionApplyState, // actual.Bri is nil, doesn't match
		},
		{
			name: "edge/actual_has_nil_xy_desired_has_xy",
			desired: Desired{
				State: State{Xy: []float32{0.5, 0.5}},
			},
			actual: Actual{
				AnyOn: true, AllOn: true,
				State: State{}, // No XY
			},
			expected: ActionApplyState,
		},
		{
			name: "edge/desired_only_cares_about_ct_other_differs",
			desired: Desired{
				State: State{Ct: Uint16Ptr(300)},
			},
			actual: Actual{
				AnyOn: true, AllOn: true,
				State: State{
					Ct:  Uint16Ptr(300),  // Matches
					Bri: Uint8Ptr(50),    // Differs but not in desired
					Hue: Uint16Ptr(1000), // Differs but not in desired
				},
			},
			expected: ActionNone, // Only compare what's in desired
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
			name:     "all_off",
			actual:   Actual{AnyOn: false, AllOn: false},
			expected: PowerStateOff,
		},
		{
			name:     "all_on",
			actual:   Actual{AnyOn: true, AllOn: true},
			expected: PowerStateOn,
		},
		{
			name:     "partial_on",
			actual:   Actual{AnyOn: true, AllOn: false},
			expected: PowerStateOn, // AnyOn is what matters
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
			desired:  Desired{State: State{On: BoolPtr(true)}},
			expected: true,
		},
		{
			name:     "power_false",
			desired:  Desired{State: State{On: BoolPtr(false)}},
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
			desired:  Desired{State: State{On: BoolPtr(true)}},
			expected: false,
		},
		{
			name:     "power_false",
			desired:  Desired{State: State{On: BoolPtr(false)}},
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
		{ActionTurnOnWithScene, "turn_on_with_scene"},
		{ActionTurnOnWithState, "turn_on_with_state"},
		{ActionTurnOff, "turn_off"},
		{ActionApplyScene, "apply_scene"},
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
