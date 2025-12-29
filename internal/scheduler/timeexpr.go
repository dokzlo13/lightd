package scheduler

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/dokzlo13/lightd/internal/geo"
)

// BaseTimeType represents the type of base time for an expression
type BaseTimeType int

const (
	BaseTimeFixed BaseTimeType = iota
	BaseTimeDawn
	BaseTimeSunrise
	BaseTimeNoon
	BaseTimeSunset
	BaseTimeDusk
)

// DSTPolicy defines how to handle DST transitions for fixed times
// TODO: Not yet implemented - currently no special DST handling is performed.
// During DST "spring forward" transitions, fixed times in the skipped hour will
// evaluate to the post-transition time (e.g., 02:30 becomes 03:30).
// During DST "fall back" transitions, fixed times in the overlap period will
// occur once (first occurrence), which may cause unexpected behavior.
type DSTPolicy int

const (
	DSTFirstOccurrence DSTPolicy = iota // Use first occurrence in overlap (default)
	DSTSecondOccurrence
	DSTSkip
	DSTShiftForward
)

// PolarPolicy defines how to handle missing sun events in polar regions
// TODO: Not yet implemented - currently uses implicit PolarSkip behavior.
// When astronomical events don't occur (e.g., no sunset during Arctic summer),
// the schedule occurrence is skipped for that day. Future implementations could
// provide fallback times.
type PolarPolicy int

const (
	PolarSkip PolarPolicy = iota // Skip that occurrence (default) - CURRENTLY IMPLEMENTED
	PolarFallbackNoon
	PolarFallbackMidnight
)

// TimeExpr represents a parsed time expression
// TODO: Add DSTPolicy and PolarPolicy fields when implementing those features
type TimeExpr struct {
	Raw       string
	BaseTime  BaseTimeType
	FixedHour int // For fixed times (0-23)
	FixedMin  int // For fixed times (0-59)
	Offset    time.Duration
}

var (
	// Match patterns like "@dawn", "@sunset", "@noon + 30m", "@sunrise - 1h30m"
	astroPattern = regexp.MustCompile(`^@(\w+)\s*([+-]\s*\d+[hms]+(?:\d+[ms]+)?)?$`)
	// Match patterns like "22:15", "06:30"
	fixedPattern = regexp.MustCompile(`^(\d{1,2}):(\d{2})$`)
	// Match duration like "30m", "1h", "1h30m"
	durationPattern = regexp.MustCompile(`([+-])\s*(.+)`)
)

// ParseTimeExpr parses a time expression string
func ParseTimeExpr(expr string) (*TimeExpr, error) {
	expr = strings.TrimSpace(expr)

	// Try fixed time pattern first
	if matches := fixedPattern.FindStringSubmatch(expr); matches != nil {
		hour, _ := strconv.Atoi(matches[1])
		min, _ := strconv.Atoi(matches[2])

		if hour < 0 || hour > 23 {
			return nil, fmt.Errorf("invalid hour: %d", hour)
		}
		if min < 0 || min > 59 {
			return nil, fmt.Errorf("invalid minute: %d", min)
		}

		return &TimeExpr{
			Raw:       expr,
			BaseTime:  BaseTimeFixed,
			FixedHour: hour,
			FixedMin:  min,
		}, nil
	}

	// Try astronomical pattern
	if matches := astroPattern.FindStringSubmatch(expr); matches != nil {
		baseTimeStr := strings.ToLower(matches[1])
		offsetStr := matches[2]

		var baseTime BaseTimeType
		switch baseTimeStr {
		case "dawn":
			baseTime = BaseTimeDawn
		case "sunrise":
			baseTime = BaseTimeSunrise
		case "noon":
			baseTime = BaseTimeNoon
		case "sunset":
			baseTime = BaseTimeSunset
		case "dusk":
			baseTime = BaseTimeDusk
		default:
			return nil, fmt.Errorf("unknown astronomical time: %s", baseTimeStr)
		}

		var offset time.Duration
		if offsetStr != "" {
			offsetStr = strings.ReplaceAll(offsetStr, " ", "")
			d, err := parseDuration(offsetStr)
			if err != nil {
				return nil, fmt.Errorf("invalid offset: %w", err)
			}
			offset = d
		}

		return &TimeExpr{
			Raw:      expr,
			BaseTime: baseTime,
			Offset:   offset,
		}, nil
	}

	return nil, fmt.Errorf("invalid time expression: %s", expr)
}

// parseDuration parses a duration string like "+30m", "-1h", "+1h30m"
func parseDuration(s string) (time.Duration, error) {
	if s == "" {
		return 0, nil
	}

	matches := durationPattern.FindStringSubmatch(s)
	if matches == nil {
		return 0, fmt.Errorf("invalid duration format: %s", s)
	}

	sign := matches[1]
	durationStr := matches[2]

	d, err := time.ParseDuration(durationStr)
	if err != nil {
		return 0, err
	}

	if sign == "-" {
		d = -d
	}

	return d, nil
}

// Evaluate calculates the actual time for this expression on a given date
func (te *TimeExpr) Evaluate(date time.Time, astro *geo.AstroTimes, tz *time.Location) (time.Time, bool) {
	var baseTime time.Time

	switch te.BaseTime {
	case BaseTimeFixed:
		baseTime = time.Date(date.Year(), date.Month(), date.Day(),
			te.FixedHour, te.FixedMin, 0, 0, tz)

	case BaseTimeDawn:
		if astro == nil || astro.Dawn.IsZero() {
			return time.Time{}, false // Polar skip
		}
		baseTime = astro.Dawn

	case BaseTimeSunrise:
		if astro == nil || astro.Sunrise.IsZero() {
			return time.Time{}, false
		}
		baseTime = astro.Sunrise

	case BaseTimeNoon:
		if astro == nil || astro.Noon.IsZero() {
			return time.Time{}, false
		}
		baseTime = astro.Noon

	case BaseTimeSunset:
		if astro == nil || astro.Sunset.IsZero() {
			return time.Time{}, false
		}
		baseTime = astro.Sunset

	case BaseTimeDusk:
		if astro == nil || astro.Dusk.IsZero() {
			return time.Time{}, false
		}
		baseTime = astro.Dusk
	}

	return baseTime.Add(te.Offset), true
}

// IsFixed returns true if this is a fixed time expression
func (te *TimeExpr) IsFixed() bool {
	return te.BaseTime == BaseTimeFixed
}

// String returns the original expression string
func (te *TimeExpr) String() string {
	return te.Raw
}

// TimeExprEvaluator evaluates time expressions using astronomical data
type TimeExprEvaluator struct {
	geo      *geo.Calculator
	location string
	timezone string
	tz       *time.Location
}

// NewTimeExprEvaluator creates a new evaluator
func NewTimeExprEvaluator(geoCalc *geo.Calculator, location, timezone string) *TimeExprEvaluator {
	tz, err := time.LoadLocation(timezone)
	if err != nil {
		tz = time.UTC
	}
	return &TimeExprEvaluator{
		geo:      geoCalc,
		location: location,
		timezone: timezone,
		tz:       tz,
	}
}

// Evaluate evaluates a time expression for a given date
func (e *TimeExprEvaluator) Evaluate(expr *TimeExpr, date time.Time) (time.Time, bool) {
	var astro *geo.AstroTimes

	if !expr.IsFixed() {
		var err error
		astro, err = e.geo.GetTimes(e.location, date, e.timezone)
		if err != nil {
			return time.Time{}, false
		}
	}

	return expr.Evaluate(date, astro, e.tz)
}

// EvaluateForToday evaluates a time expression for today
func (e *TimeExprEvaluator) EvaluateForToday(expr *TimeExpr) (time.Time, bool) {
	today := time.Now().In(e.tz)
	return e.Evaluate(expr, today)
}

// ComputeNextOccurrence finds the next occurrence of a time expression after the given time
func (e *TimeExprEvaluator) ComputeNextOccurrence(expr *TimeExpr, after time.Time) (time.Time, bool) {
	// Start from today
	date := after.In(e.tz)

	// Check up to 366 days ahead (handles leap years)
	for i := 0; i < 366; i++ {
		checkDate := date.AddDate(0, 0, i)
		t, ok := e.Evaluate(expr, checkDate)
		if !ok {
			continue // Skip days where expression doesn't evaluate (polar)
		}

		if t.After(after) {
			return t, true
		}
	}

	return time.Time{}, false
}

// ComputePrevOccurrence finds the previous occurrence of a time expression before the given time
func (e *TimeExprEvaluator) ComputePrevOccurrence(expr *TimeExpr, before time.Time) (time.Time, bool) {
	// Start from today
	date := before.In(e.tz)

	// Check up to 366 days back
	for i := 0; i < 366; i++ {
		checkDate := date.AddDate(0, 0, -i)
		t, ok := e.Evaluate(expr, checkDate)
		if !ok {
			continue
		}

		if t.Before(before) {
			return t, true
		}
	}

	return time.Time{}, false
}

// Timezone returns the evaluator's timezone
func (e *TimeExprEvaluator) Timezone() *time.Location {
	return e.tz
}

