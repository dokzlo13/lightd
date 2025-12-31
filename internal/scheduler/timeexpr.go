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

// TimeExpr represents a parsed time expression
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

// IsAstronomical returns true if this is an astronomical time expression
func (te *TimeExpr) IsAstronomical() bool {
	return te.BaseTime != BaseTimeFixed
}

// String returns the original expression string
func (te *TimeExpr) String() string {
	return te.Raw
}

// TimeEvaluator is the interface for evaluating time expressions
type TimeEvaluator interface {
	// Evaluate evaluates a time expression for a given date
	Evaluate(expr *TimeExpr, date time.Time) (time.Time, bool)

	// ComputeNextOccurrence finds the next occurrence after the given time
	ComputeNextOccurrence(expr *TimeExpr, after time.Time) (time.Time, bool)

	// ComputePrevOccurrence finds the previous occurrence before the given time
	ComputePrevOccurrence(expr *TimeExpr, before time.Time) (time.Time, bool)

	// Timezone returns the evaluator's timezone
	Timezone() *time.Location

	// SupportsAstronomical returns whether this evaluator supports astronomical times
	SupportsAstronomical() bool
}

// AstroTimeEvaluator evaluates time expressions with full astronomical support
type AstroTimeEvaluator struct {
	geo      *geo.Calculator
	location string
	timezone string
	tz       *time.Location
}

// NewAstroTimeEvaluator creates a new evaluator with astronomical time support
func NewAstroTimeEvaluator(geoCalc *geo.Calculator, location, timezone string) *AstroTimeEvaluator {
	tz, err := time.LoadLocation(timezone)
	if err != nil {
		tz = time.UTC
	}
	return &AstroTimeEvaluator{
		geo:      geoCalc,
		location: location,
		timezone: timezone,
		tz:       tz,
	}
}

func (e *AstroTimeEvaluator) SupportsAstronomical() bool { return true }
func (e *AstroTimeEvaluator) Timezone() *time.Location   { return e.tz }

func (e *AstroTimeEvaluator) Evaluate(expr *TimeExpr, date time.Time) (time.Time, bool) {
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

func (e *AstroTimeEvaluator) ComputeNextOccurrence(expr *TimeExpr, after time.Time) (time.Time, bool) {
	date := after.In(e.tz)

	for i := 0; i < 366; i++ {
		checkDate := date.AddDate(0, 0, i)
		t, ok := e.Evaluate(expr, checkDate)
		if !ok {
			continue
		}
		if t.After(after) {
			return t, true
		}
	}

	return time.Time{}, false
}

func (e *AstroTimeEvaluator) ComputePrevOccurrence(expr *TimeExpr, before time.Time) (time.Time, bool) {
	date := before.In(e.tz)

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

// FixedTimeEvaluator evaluates only fixed time expressions (no geo required)
type FixedTimeEvaluator struct {
	tz *time.Location
}

// NewFixedTimeEvaluator creates a new evaluator for fixed times only
func NewFixedTimeEvaluator(timezone string) *FixedTimeEvaluator {
	tz, err := time.LoadLocation(timezone)
	if err != nil {
		tz = time.UTC
	}
	return &FixedTimeEvaluator{tz: tz}
}

func (e *FixedTimeEvaluator) SupportsAstronomical() bool { return false }
func (e *FixedTimeEvaluator) Timezone() *time.Location   { return e.tz }

func (e *FixedTimeEvaluator) Evaluate(expr *TimeExpr, date time.Time) (time.Time, bool) {
	if !expr.IsFixed() {
		// Cannot evaluate astronomical expressions
		return time.Time{}, false
	}
	return expr.Evaluate(date, nil, e.tz)
}

func (e *FixedTimeEvaluator) ComputeNextOccurrence(expr *TimeExpr, after time.Time) (time.Time, bool) {
	if !expr.IsFixed() {
		return time.Time{}, false
	}

	date := after.In(e.tz)

	// For fixed times, just check today and tomorrow
	for i := 0; i < 2; i++ {
		checkDate := date.AddDate(0, 0, i)
		t, ok := e.Evaluate(expr, checkDate)
		if !ok {
			continue
		}
		if t.After(after) {
			return t, true
		}
	}

	return time.Time{}, false
}

func (e *FixedTimeEvaluator) ComputePrevOccurrence(expr *TimeExpr, before time.Time) (time.Time, bool) {
	if !expr.IsFixed() {
		return time.Time{}, false
	}

	date := before.In(e.tz)

	// For fixed times, just check today and yesterday
	for i := 0; i < 2; i++ {
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
