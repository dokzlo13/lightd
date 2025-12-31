// Package scheduler provides a polymorphic schedule system.
// Different schedule types (daily, periodic, once) implement the Schedule interface.
package scheduler

import (
	"fmt"
	"time"
)

// Schedule is the core abstraction for any source of timed events.
// Different schedule types implement this interface to provide their timing logic.
type Schedule interface {
	// ID returns the unique identifier for this schedule
	ID() string

	// Tag returns the optional tag for grouping schedules
	Tag() string

	// Next returns the next occurrence after the given time, or nil if none
	Next(after time.Time) *Occurrence

	// Prev returns the previous occurrence before the given time, or nil if none
	Prev(before time.Time) *Occurrence

	// ActionName returns the name of the action to invoke
	ActionName() string

	// ActionArgs returns the arguments to pass to the action
	ActionArgs() map[string]any

	// MisfirePolicy returns how to handle missed occurrences on boot
	MisfirePolicy() MisfirePolicy
}

// Occurrence represents a specific firing point of a schedule
type Occurrence struct {
	// ID uniquely identifies this occurrence (e.g., "scene:dawn/1704067200")
	ID string

	// ScheduleID is the ID of the schedule that created this occurrence
	ScheduleID string

	// Time is when this occurrence should fire
	Time time.Time
}

// NewOccurrence creates a new occurrence with a standard ID format
func NewOccurrence(scheduleID string, t time.Time) *Occurrence {
	return &Occurrence{
		ID:         fmt.Sprintf("%s/%d", scheduleID, t.Unix()),
		ScheduleID: scheduleID,
		Time:       t,
	}
}

// NewOccurrenceWithSuffix creates an occurrence with a custom suffix (e.g., for boot recovery)
func NewOccurrenceWithSuffix(scheduleID string, t time.Time, suffix string) *Occurrence {
	return &Occurrence{
		ID:         fmt.Sprintf("%s/%s/%d", scheduleID, suffix, t.Unix()),
		ScheduleID: scheduleID,
		Time:       t,
	}
}

// DailySchedule implements Schedule for time-of-day based schedules.
// Supports fixed times (e.g., "20:45") and astronomical times (e.g., "@dawn", "@sunset - 30m").
type DailySchedule struct {
	id            string
	tag           string
	timeExpr      *TimeExpr
	evaluator     TimeEvaluator
	actionName    string
	actionArgs    map[string]any
	misfirePolicy MisfirePolicy
}

// NewDailySchedule creates a new daily schedule from a time expression.
// Returns an error if the expression uses astronomical times but the evaluator doesn't support them.
func NewDailySchedule(
	id string,
	timeExprStr string,
	actionName string,
	actionArgs map[string]any,
	tag string,
	misfirePolicy MisfirePolicy,
	evaluator TimeEvaluator,
) (*DailySchedule, error) {
	expr, err := ParseTimeExpr(timeExprStr)
	if err != nil {
		return nil, fmt.Errorf("invalid time expression: %w", err)
	}

	// Fail early if using astronomical times without support
	if expr.IsAstronomical() && !evaluator.SupportsAstronomical() {
		return nil, fmt.Errorf("astronomical time expression %q requires geo to be enabled (events.scheduler.geo.enabled: true)", timeExprStr)
	}

	return &DailySchedule{
		id:            id,
		tag:           tag,
		timeExpr:      expr,
		evaluator:     evaluator,
		actionName:    actionName,
		actionArgs:    actionArgs,
		misfirePolicy: misfirePolicy,
	}, nil
}

func (s *DailySchedule) ID() string                   { return s.id }
func (s *DailySchedule) Tag() string                  { return s.tag }
func (s *DailySchedule) ActionName() string           { return s.actionName }
func (s *DailySchedule) ActionArgs() map[string]any   { return s.actionArgs }
func (s *DailySchedule) MisfirePolicy() MisfirePolicy { return s.misfirePolicy }

// Next returns the next occurrence after the given time.
func (s *DailySchedule) Next(after time.Time) *Occurrence {
	t, ok := s.evaluator.ComputeNextOccurrence(s.timeExpr, after)
	if !ok {
		return nil
	}
	return NewOccurrence(s.id, t)
}

// Prev returns the previous occurrence before the given time.
func (s *DailySchedule) Prev(before time.Time) *Occurrence {
	t, ok := s.evaluator.ComputePrevOccurrence(s.timeExpr, before)
	if !ok {
		return nil
	}
	return NewOccurrence(s.id, t)
}

// TimeExprString returns the original time expression string for display.
func (s *DailySchedule) TimeExprString() string {
	return s.timeExpr.String()
}

// PeriodicSchedule implements Schedule for interval-based schedules.
// Fires at regular intervals (e.g., "every 30m").
type PeriodicSchedule struct {
	id            string
	tag           string
	interval      time.Duration
	startTime     time.Time // When the schedule started (for interval calculation)
	actionName    string
	actionArgs    map[string]any
	misfirePolicy MisfirePolicy
}

// NewPeriodicSchedule creates a new periodic schedule.
func NewPeriodicSchedule(
	id string,
	interval time.Duration,
	actionName string,
	actionArgs map[string]any,
	tag string,
) *PeriodicSchedule {
	return &PeriodicSchedule{
		id:            id,
		tag:           tag,
		interval:      interval,
		startTime:     time.Now(),
		actionName:    actionName,
		actionArgs:    actionArgs,
		misfirePolicy: MisfirePolicySkip, // Periodics don't replay missed
	}
}

func (s *PeriodicSchedule) ID() string                   { return s.id }
func (s *PeriodicSchedule) Tag() string                  { return s.tag }
func (s *PeriodicSchedule) ActionName() string           { return s.actionName }
func (s *PeriodicSchedule) ActionArgs() map[string]any   { return s.actionArgs }
func (s *PeriodicSchedule) MisfirePolicy() MisfirePolicy { return s.misfirePolicy }

// Next returns the next occurrence after the given time.
func (s *PeriodicSchedule) Next(after time.Time) *Occurrence {
	if after.Before(s.startTime) {
		return NewOccurrence(s.id, s.startTime)
	}

	elapsed := after.Sub(s.startTime)
	ticks := int64(elapsed / s.interval)
	nextTime := s.startTime.Add(time.Duration(ticks+1) * s.interval)

	return NewOccurrence(s.id, nextTime)
}

// Prev returns the previous occurrence before the given time.
func (s *PeriodicSchedule) Prev(before time.Time) *Occurrence {
	if before.Before(s.startTime) || before.Equal(s.startTime) {
		return nil
	}

	elapsed := before.Sub(s.startTime)
	ticks := int64(elapsed / s.interval)
	prevTime := s.startTime.Add(time.Duration(ticks) * s.interval)

	// If we're exactly on a tick, go to previous
	if prevTime.Equal(before) && ticks > 0 {
		prevTime = prevTime.Add(-s.interval)
	}

	if prevTime.Before(s.startTime) {
		return nil
	}

	return NewOccurrence(s.id, prevTime)
}

// Interval returns the schedule interval for display.
func (s *PeriodicSchedule) Interval() time.Duration {
	return s.interval
}
