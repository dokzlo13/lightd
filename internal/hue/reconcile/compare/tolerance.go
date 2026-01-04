// Package compare provides tolerance-based comparison helpers for Hue state reconciliation.
package compare

import "math"

// Tolerance constants for different value types.
// These are calibrated based on Hue bridge behavior and rounding.
const (
	// BriTolerance is the tolerance for brightness values (0-254).
	BriTolerance uint8 = 2

	// SatTolerance is the tolerance for saturation values (0-254).
	SatTolerance uint8 = 2

	// CtTolerance is the tolerance for color temperature in mirek (153-500).
	CtTolerance uint16 = 5

	// HueTolerance is the tolerance for hue values (0-65535).
	HueTolerance uint16 = 100

	// XyTolerance is the tolerance for CIE XY color coordinates.
	XyTolerance float32 = 0.01
)

// Uint8Near returns true if a and b are within tolerance of each other.
// Safe for unsigned subtraction (no overflow).
func Uint8Near(a, b uint8, tolerance uint8) bool {
	if a > b {
		return a-b <= tolerance
	}
	return b-a <= tolerance
}

// Uint16Near returns true if a and b are within tolerance of each other.
// Safe for unsigned subtraction (no overflow).
func Uint16Near(a, b uint16, tolerance uint16) bool {
	if a > b {
		return a-b <= tolerance
	}
	return b-a <= tolerance
}

// Float32Near returns true if a and b are within tolerance of each other.
func Float32Near(a, b float32, tolerance float32) bool {
	return math.Abs(float64(a-b)) <= float64(tolerance)
}

// BriMatches returns true if two brightness values match within tolerance.
func BriMatches(desired, actual uint8) bool {
	return Uint8Near(desired, actual, BriTolerance)
}

// SatMatches returns true if two saturation values match within tolerance.
func SatMatches(desired, actual uint8) bool {
	return Uint8Near(desired, actual, SatTolerance)
}

// CtMatches returns true if two color temperature values match within tolerance.
func CtMatches(desired, actual uint16) bool {
	return Uint16Near(desired, actual, CtTolerance)
}

// HueMatches returns true if two hue values match within tolerance.
func HueMatches(desired, actual uint16) bool {
	return Uint16Near(desired, actual, HueTolerance)
}

// XyMatches returns true if two XY coordinate pairs match within tolerance.
// Returns false if either slice has fewer than 2 elements.
func XyMatches(desired, actual []float32) bool {
	if len(desired) < 2 || len(actual) < 2 {
		return false
	}
	return Float32Near(desired[0], actual[0], XyTolerance) &&
		Float32Near(desired[1], actual[1], XyTolerance)
}
