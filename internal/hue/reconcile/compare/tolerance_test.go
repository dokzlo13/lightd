package compare

import (
	"testing"
)

func TestUint8Near(t *testing.T) {
	tests := []struct {
		name      string
		a, b      uint8
		tolerance uint8
		expected  bool
	}{
		// Exact matches
		{"exact_zero", 0, 0, 2, true},
		{"exact_mid", 100, 100, 2, true},
		{"exact_max", 254, 254, 2, true},

		// Within tolerance (a > b)
		{"within_a_gt_b_by_1", 100, 99, 2, true},
		{"within_a_gt_b_by_2", 100, 98, 2, true},

		// Within tolerance (b > a)
		{"within_b_gt_a_by_1", 99, 100, 2, true},
		{"within_b_gt_a_by_2", 98, 100, 2, true},

		// Outside tolerance
		{"outside_a_gt_b", 100, 97, 2, false},
		{"outside_b_gt_a", 97, 100, 2, false},

		// Edge cases - boundary values
		{"edge_near_zero", 0, 2, 2, true},
		{"edge_near_zero_fail", 0, 3, 2, false},
		{"edge_near_max", 254, 252, 2, true},
		{"edge_near_max_fail", 254, 251, 2, false},

		// Zero tolerance
		{"zero_tolerance_match", 50, 50, 0, true},
		{"zero_tolerance_fail", 50, 51, 0, false},

		// Large tolerance
		{"large_tolerance", 0, 254, 255, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Uint8Near(tt.a, tt.b, tt.tolerance)
			if got != tt.expected {
				t.Errorf("Uint8Near(%d, %d, %d) = %v, want %v",
					tt.a, tt.b, tt.tolerance, got, tt.expected)
			}
		})
	}
}

func TestUint16Near(t *testing.T) {
	tests := []struct {
		name      string
		a, b      uint16
		tolerance uint16
		expected  bool
	}{
		// Exact matches
		{"exact_zero", 0, 0, 5, true},
		{"exact_mid", 30000, 30000, 100, true},
		{"exact_max", 65535, 65535, 100, true},

		// Within tolerance (a > b)
		{"within_a_gt_b", 30000, 29950, 100, true},
		{"within_a_gt_b_exact", 30000, 29900, 100, true},

		// Within tolerance (b > a)
		{"within_b_gt_a", 29950, 30000, 100, true},
		{"within_b_gt_a_exact", 29900, 30000, 100, true},

		// Outside tolerance
		{"outside_a_gt_b", 30000, 29899, 100, false},
		{"outside_b_gt_a", 29899, 30000, 100, false},

		// Color temperature range (153-500 mirek)
		{"ct_match", 300, 304, 5, true},
		{"ct_fail", 300, 310, 5, false},

		// Hue range (0-65535)
		{"hue_match", 32000, 32099, 100, true},
		{"hue_fail", 32000, 32150, 100, false},

		// Edge cases
		{"edge_near_zero", 0, 5, 5, true},
		{"edge_near_zero_fail", 0, 6, 5, false},
		{"edge_near_max", 65535, 65530, 5, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Uint16Near(tt.a, tt.b, tt.tolerance)
			if got != tt.expected {
				t.Errorf("Uint16Near(%d, %d, %d) = %v, want %v",
					tt.a, tt.b, tt.tolerance, got, tt.expected)
			}
		})
	}
}

func TestFloat32Near(t *testing.T) {
	tests := []struct {
		name      string
		a, b      float32
		tolerance float32
		expected  bool
	}{
		// Exact matches
		{"exact_zero", 0.0, 0.0, 0.01, true},
		{"exact_mid", 0.5, 0.5, 0.01, true},
		{"exact_one", 1.0, 1.0, 0.01, true},

		// Within tolerance
		{"within_positive", 0.5, 0.505, 0.01, true},
		{"within_negative", 0.5, 0.495, 0.01, true},
		{"within_exact_tolerance", 0.5, 0.51, 0.01, true},

		// Outside tolerance
		{"outside_positive", 0.5, 0.52, 0.01, false},
		{"outside_negative", 0.5, 0.48, 0.01, false},

		// Negative values
		{"negative_within", -0.5, -0.505, 0.01, true},
		{"negative_outside", -0.5, -0.52, 0.01, false},

		// Cross zero
		{"cross_zero_within", -0.005, 0.005, 0.01, true},
		{"cross_zero_outside", -0.02, 0.02, 0.01, false},

		// Very small tolerance
		{"tiny_tolerance_match", 0.5, 0.5001, 0.001, true},
		{"tiny_tolerance_fail", 0.5, 0.502, 0.001, false},

		// Zero tolerance
		{"zero_tolerance_match", 0.5, 0.5, 0.0, true},
		{"zero_tolerance_fail", 0.5, 0.500001, 0.0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Float32Near(tt.a, tt.b, tt.tolerance)
			if got != tt.expected {
				t.Errorf("Float32Near(%f, %f, %f) = %v, want %v",
					tt.a, tt.b, tt.tolerance, got, tt.expected)
			}
		})
	}
}

func TestBriMatches(t *testing.T) {
	tests := []struct {
		name            string
		desired, actual uint8
		expected        bool
	}{
		{"exact", 200, 200, true},
		{"within_plus", 200, 201, true},
		{"within_minus", 200, 198, true},
		{"outside", 200, 196, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BriMatches(tt.desired, tt.actual)
			if got != tt.expected {
				t.Errorf("BriMatches(%d, %d) = %v, want %v",
					tt.desired, tt.actual, got, tt.expected)
			}
		})
	}
}

func TestSatMatches(t *testing.T) {
	tests := []struct {
		name            string
		desired, actual uint8
		expected        bool
	}{
		{"exact", 200, 200, true},
		{"within", 200, 202, true},
		{"outside", 200, 205, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SatMatches(tt.desired, tt.actual)
			if got != tt.expected {
				t.Errorf("SatMatches(%d, %d) = %v, want %v",
					tt.desired, tt.actual, got, tt.expected)
			}
		})
	}
}

func TestCtMatches(t *testing.T) {
	tests := []struct {
		name            string
		desired, actual uint16
		expected        bool
	}{
		{"exact", 300, 300, true},
		{"within", 300, 304, true},
		{"outside", 300, 310, false},
		{"min_ct", 153, 158, true},
		{"max_ct", 500, 495, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CtMatches(tt.desired, tt.actual)
			if got != tt.expected {
				t.Errorf("CtMatches(%d, %d) = %v, want %v",
					tt.desired, tt.actual, got, tt.expected)
			}
		})
	}
}

func TestHueMatches(t *testing.T) {
	tests := []struct {
		name            string
		desired, actual uint16
		expected        bool
	}{
		{"exact", 30000, 30000, true},
		{"within", 30000, 30099, true},
		{"outside", 30000, 30150, false},
		{"near_zero", 0, 99, true},
		{"near_max", 65535, 65435, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := HueMatches(tt.desired, tt.actual)
			if got != tt.expected {
				t.Errorf("HueMatches(%d, %d) = %v, want %v",
					tt.desired, tt.actual, got, tt.expected)
			}
		})
	}
}

func TestXyMatches(t *testing.T) {
	tests := []struct {
		name            string
		desired, actual []float32
		expected        bool
	}{
		// Valid matches
		{"exact", []float32{0.5, 0.4}, []float32{0.5, 0.4}, true},
		{"within_tolerance", []float32{0.5, 0.4}, []float32{0.505, 0.395}, true},
		{"exact_tolerance_boundary", []float32{0.5, 0.4}, []float32{0.51, 0.41}, true},

		// Outside tolerance
		{"x_outside", []float32{0.5, 0.4}, []float32{0.52, 0.4}, false},
		{"y_outside", []float32{0.5, 0.4}, []float32{0.5, 0.42}, false},
		{"both_outside", []float32{0.5, 0.4}, []float32{0.52, 0.42}, false},

		// Invalid inputs
		{"desired_empty", []float32{}, []float32{0.5, 0.4}, false},
		{"actual_empty", []float32{0.5, 0.4}, []float32{}, false},
		{"desired_one_element", []float32{0.5}, []float32{0.5, 0.4}, false},
		{"actual_one_element", []float32{0.5, 0.4}, []float32{0.5}, false},
		{"both_empty", []float32{}, []float32{}, false},
		{"desired_nil", nil, []float32{0.5, 0.4}, false},
		{"actual_nil", []float32{0.5, 0.4}, nil, false},

		// Extra elements (should still work - only first two matter)
		{"extra_elements", []float32{0.5, 0.4, 0.9}, []float32{0.5, 0.4, 0.1}, true},

		// Edge values
		{"near_zero", []float32{0.0, 0.0}, []float32{0.005, 0.005}, true},
		{"near_one", []float32{1.0, 1.0}, []float32{0.995, 0.995}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := XyMatches(tt.desired, tt.actual)
			if got != tt.expected {
				t.Errorf("XyMatches(%v, %v) = %v, want %v",
					tt.desired, tt.actual, got, tt.expected)
			}
		})
	}
}

func TestToleranceConstants(t *testing.T) {
	// Verify tolerance constants are set to expected values
	if BriTolerance != 2 {
		t.Errorf("BriTolerance = %d, want 2", BriTolerance)
	}
	if SatTolerance != 2 {
		t.Errorf("SatTolerance = %d, want 2", SatTolerance)
	}
	if CtTolerance != 5 {
		t.Errorf("CtTolerance = %d, want 5", CtTolerance)
	}
	if HueTolerance != 100 {
		t.Errorf("HueTolerance = %d, want 100", HueTolerance)
	}
	if XyTolerance != 0.01 {
		t.Errorf("XyTolerance = %f, want 0.01", XyTolerance)
	}
}
