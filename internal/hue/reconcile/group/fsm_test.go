package group

import (
	"testing"
)

// Helper to create a bool pointer
func boolPtr(b bool) *bool {
	return &b
}

// Helper to create a uint8 pointer
func uint8Ptr(v uint8) *uint8 {
	return &v
}

// Helper to create a uint16 pointer
func uint16Ptr(v uint16) *uint16 {
	return &v
}

func TestDetermineAction(t *testing.T) {
	tests := []struct {
		name     string
		desired  Desired
		actual   Actual
		expected Action
	}{
		// === Group OFF cases ===
		{
			name:     "off/no_desired_state",
			desired:  Desired{},
			actual:   Actual{AnyOn: false, AllOn: false},
			expected: ActionNone,
		},
		{
			name:     "off/wants_power_on_with_scene",
			desired:  Desired{Power: boolPtr(true), SceneName: "Relax"},
			actual:   Actual{AnyOn: false, AllOn: false},
			expected: ActionTurnOnWithScene,
		},
		{
			name:     "off/wants_power_on_with_brightness",
			desired:  Desired{Power: boolPtr(true), Bri: uint8Ptr(254)},
			actual:   Actual{AnyOn: false, AllOn: false},
			expected: ActionTurnOnWithState,
		},
		{
			name:     "off/wants_power_on_with_color_temp",
			desired:  Desired{Power: boolPtr(true), Ct: uint16Ptr(300)},
			actual:   Actual{AnyOn: false, AllOn: false},
			expected: ActionTurnOnWithState,
		},
		{
			name:     "off/wants_power_on_with_hue_sat",
			desired:  Desired{Power: boolPtr(true), Hue: uint16Ptr(10000), Sat: uint8Ptr(200)},
			actual:   Actual{AnyOn: false, AllOn: false},
			expected: ActionTurnOnWithState,
		},
		{
			name:     "off/wants_power_on_with_xy",
			desired:  Desired{Power: boolPtr(true), Xy: []float32{0.5, 0.5}},
			actual:   Actual{AnyOn: false, AllOn: false},
			expected: ActionTurnOnWithState,
		},
		{
			name:     "off/wants_power_on_no_properties",
			desired:  Desired{Power: boolPtr(true)},
			actual:   Actual{AnyOn: false, AllOn: false},
			expected: ActionNone, // Can't turn on without something to apply
		},
		{
			name:     "off/wants_power_off",
			desired:  Desired{Power: boolPtr(false)},
			actual:   Actual{AnyOn: false, AllOn: false},
			expected: ActionNone, // Already off
		},
		{
			name:     "off/scene_without_power_request",
			desired:  Desired{SceneName: "Relax"},
			actual:   Actual{AnyOn: false, AllOn: false},
			expected: ActionNone, // No explicit power on request
		},

		// === Group ON cases ===
		{
			name:     "on/no_desired_state",
			desired:  Desired{},
			actual:   Actual{AnyOn: true, AllOn: true},
			expected: ActionNone,
		},
		{
			name:     "on/wants_power_off",
			desired:  Desired{Power: boolPtr(false)},
			actual:   Actual{AnyOn: true, AllOn: true},
			expected: ActionTurnOff,
		},
		{
			name:     "on/wants_power_off_with_scene",
			desired:  Desired{Power: boolPtr(false), SceneName: "Relax"},
			actual:   Actual{AnyOn: true, AllOn: true},
			expected: ActionTurnOff, // Power off takes priority over scene
		},
		{
			name:     "on/has_scene",
			desired:  Desired{SceneName: "Relax"},
			actual:   Actual{AnyOn: true, AllOn: true},
			expected: ActionApplyScene,
		},
		{
			name:     "on/has_scene_and_power_on",
			desired:  Desired{Power: boolPtr(true), SceneName: "Energize"},
			actual:   Actual{AnyOn: true, AllOn: true},
			expected: ActionApplyScene,
		},
		{
			name:     "on/has_brightness",
			desired:  Desired{Bri: uint8Ptr(128)},
			actual:   Actual{AnyOn: true, AllOn: true},
			expected: ActionApplyState,
		},
		{
			name:     "on/has_color_temp",
			desired:  Desired{Ct: uint16Ptr(400)},
			actual:   Actual{AnyOn: true, AllOn: true},
			expected: ActionApplyState,
		},
		{
			name:     "on/has_hue",
			desired:  Desired{Hue: uint16Ptr(30000)},
			actual:   Actual{AnyOn: true, AllOn: true},
			expected: ActionApplyState,
		},
		{
			name:     "on/has_saturation",
			desired:  Desired{Sat: uint8Ptr(200)},
			actual:   Actual{AnyOn: true, AllOn: true},
			expected: ActionApplyState,
		},
		{
			name:     "on/has_xy",
			desired:  Desired{Xy: []float32{0.3, 0.4}},
			actual:   Actual{AnyOn: true, AllOn: true},
			expected: ActionApplyState,
		},
		{
			name:     "on/scene_takes_priority_over_color",
			desired:  Desired{SceneName: "Concentrate", Bri: uint8Ptr(200)},
			actual:   Actual{AnyOn: true, AllOn: true},
			expected: ActionApplyScene, // Scene has priority
		},
		{
			name:     "on/wants_power_on",
			desired:  Desired{Power: boolPtr(true)},
			actual:   Actual{AnyOn: true, AllOn: true},
			expected: ActionNone, // Already on, no scene or color to apply
		},

		// === Partial ON cases (AnyOn=true, AllOn=false) ===
		{
			name:     "partial_on/has_scene",
			desired:  Desired{SceneName: "Relax"},
			actual:   Actual{AnyOn: true, AllOn: false},
			expected: ActionApplyScene,
		},
		{
			name:     "partial_on/wants_power_off",
			desired:  Desired{Power: boolPtr(false)},
			actual:   Actual{AnyOn: true, AllOn: false},
			expected: ActionTurnOff,
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

func TestDeriveState(t *testing.T) {
	tests := []struct {
		name     string
		actual   Actual
		expected State
	}{
		{
			name:     "all_off",
			actual:   Actual{AnyOn: false, AllOn: false},
			expected: StateOff,
		},
		{
			name:     "all_on",
			actual:   Actual{AnyOn: true, AllOn: true},
			expected: StateOn,
		},
		{
			name:     "partial_on",
			actual:   Actual{AnyOn: true, AllOn: false},
			expected: StateOn, // AnyOn is what matters
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := deriveState(tt.actual)
			if got != tt.expected {
				t.Errorf("deriveState() = %v, want %v", got, tt.expected)
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
			desired:  Desired{Power: boolPtr(true)},
			expected: true,
		},
		{
			name:     "power_false",
			desired:  Desired{Power: boolPtr(false)},
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
			desired:  Desired{Power: boolPtr(true)},
			expected: false,
		},
		{
			name:     "power_false",
			desired:  Desired{Power: boolPtr(false)},
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
		desired  Desired
		expected bool
	}{
		{
			name:     "empty",
			desired:  Desired{},
			expected: false,
		},
		{
			name:     "only_power",
			desired:  Desired{Power: boolPtr(true)},
			expected: false,
		},
		{
			name:     "only_scene",
			desired:  Desired{SceneName: "Relax"},
			expected: false,
		},
		{
			name:     "has_bri",
			desired:  Desired{Bri: uint8Ptr(128)},
			expected: true,
		},
		{
			name:     "has_hue",
			desired:  Desired{Hue: uint16Ptr(10000)},
			expected: true,
		},
		{
			name:     "has_sat",
			desired:  Desired{Sat: uint8Ptr(200)},
			expected: true,
		},
		{
			name:     "has_xy",
			desired:  Desired{Xy: []float32{0.5, 0.5}},
			expected: true,
		},
		{
			name:     "has_ct",
			desired:  Desired{Ct: uint16Ptr(300)},
			expected: true,
		},
		{
			name:     "has_multiple",
			desired:  Desired{Bri: uint8Ptr(200), Ct: uint16Ptr(400)},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hasColorProperties(tt.desired)
			if got != tt.expected {
				t.Errorf("hasColorProperties() = %v, want %v", got, tt.expected)
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
