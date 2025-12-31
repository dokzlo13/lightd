package scheduler

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/dokzlo13/lightd/internal/eventbus"
	"github.com/dokzlo13/lightd/internal/geo"
	"github.com/dokzlo13/lightd/internal/ledger"
)

// Strategy for finding closest schedule
type Strategy string

const (
	StrategyNext Strategy = "NEXT"
	StrategyPrev Strategy = "PREV"
)

// Scheduler manages schedule definitions and occurrence execution.
// Schedules are stored in memory and events are emitted to the EventBus.
type Scheduler struct {
	mu        sync.RWMutex
	schedules map[string]Schedule

	bus       *eventbus.Bus
	ledger    *ledger.Ledger
	evaluator TimeEvaluator
	tz        *time.Location

	reschedule chan struct{}
}

// New creates a new scheduler with full astronomical time support
func New(
	bus *eventbus.Bus,
	l *ledger.Ledger,
	geoCalc *geo.Calculator,
	location, timezone string,
) *Scheduler {
	tz, err := time.LoadLocation(timezone)
	if err != nil {
		log.Warn().Err(err).Str("timezone", timezone).Msg("Failed to load timezone, using UTC")
		tz = time.UTC
	}

	return &Scheduler{
		schedules:  make(map[string]Schedule),
		bus:        bus,
		ledger:     l,
		evaluator:  NewAstroTimeEvaluator(geoCalc, location, timezone),
		tz:         tz,
		reschedule: make(chan struct{}, 1),
	}
}

// NewWithFixedTimeOnly creates a scheduler that only supports fixed times (no geo)
func NewWithFixedTimeOnly(
	bus *eventbus.Bus,
	l *ledger.Ledger,
	timezone string,
) *Scheduler {
	tz, err := time.LoadLocation(timezone)
	if err != nil {
		log.Warn().Err(err).Str("timezone", timezone).Msg("Failed to load timezone, using UTC")
		tz = time.UTC
	}

	return &Scheduler{
		schedules:  make(map[string]Schedule),
		bus:        bus,
		ledger:     l,
		evaluator:  NewFixedTimeEvaluator(timezone),
		tz:         tz,
		reschedule: make(chan struct{}, 1),
	}
}

// Register adds a schedule
func (s *Scheduler) Register(sched Schedule) {
	s.mu.Lock()
	s.schedules[sched.ID()] = sched
	s.mu.Unlock()

	log.Debug().
		Str("id", sched.ID()).
		Str("tag", sched.Tag()).
		Str("action", sched.ActionName()).
		Msg("Schedule registered")

	s.notifyReschedule()
}

// Unregister removes a schedule
func (s *Scheduler) Unregister(id string) {
	s.mu.Lock()
	delete(s.schedules, id)
	s.mu.Unlock()
	s.notifyReschedule()
}

// Define creates and registers a daily schedule (convenience method for Lua)
func (s *Scheduler) Define(id, timeExpr, actionName string, args map[string]any, tag string, misfirePolicy MisfirePolicy) error {
	sched, err := NewDailySchedule(id, timeExpr, actionName, args, tag, misfirePolicy, s.evaluator)
	if err != nil {
		return err
	}
	s.Register(sched)
	return nil
}

// DefinePeriodic creates and registers a periodic schedule (convenience method for Lua)
func (s *Scheduler) DefinePeriodic(id string, interval time.Duration, actionName string, args map[string]any, tag string) {
	sched := NewPeriodicSchedule(id, interval, actionName, args, tag)
	s.Register(sched)
}

// notifyReschedule signals the scheduler to recalculate
func (s *Scheduler) notifyReschedule() {
	select {
	case s.reschedule <- struct{}{}:
	default:
	}
}

// Run starts the scheduler loop
func (s *Scheduler) Run(ctx context.Context) error {
	log.Info().Msg("Scheduler started")

	for {
		occ, sched := s.nextOccurrence(time.Now())

		sleepDuration := time.Hour // default if no schedules
		if occ != nil {
			sleepDuration = time.Until(occ.Time)
			if sleepDuration < 0 {
				sleepDuration = 0
			}
		}

		log.Debug().
			Dur("sleep_duration", sleepDuration).
			Msg("Scheduler sleeping")

		timer := time.NewTimer(sleepDuration)

		select {
		case <-ctx.Done():
			timer.Stop()
			log.Info().Msg("Scheduler stopping")
			return nil

		case <-s.reschedule:
			timer.Stop()
			log.Debug().Msg("Schedule changed, recomputing")
			continue

		case <-timer.C:
			if occ != nil && sched != nil {
				s.emit(sched, occ, "scheduler")
			}
		}
	}
}

// RunBootRecovery runs the most recent previous occurrence for schedules,
// grouped by tag. For schedules with the same tag, only the one with the
// most recent previous occurrence is executed (since later schedules supersede earlier ones).
// Schedules without a tag are grouped individually.
func (s *Scheduler) RunBootRecovery() {
	s.mu.RLock()
	defer s.mu.RUnlock()

	now := time.Now()

	// Group schedules by tag (or by ID if no tag)
	// For each group, find the schedule with the most recent previous occurrence
	type candidate struct {
		sched Schedule
		prev  *Occurrence
	}
	winners := make(map[string]candidate)

	for _, sched := range s.schedules {
		if sched.MisfirePolicy() == MisfirePolicySkip {
			continue
		}

		prev := sched.Prev(now)
		if prev == nil {
			continue
		}

		// Group key: use tag if present, otherwise use schedule ID (each untagged schedule is its own group)
		groupKey := sched.Tag()
		if groupKey == "" {
			groupKey = "__untagged:" + sched.ID()
		}

		existing, exists := winners[groupKey]
		if !exists || prev.Time.After(existing.prev.Time) {
			winners[groupKey] = candidate{sched: sched, prev: prev}
		}
	}

	// Emit events for the winning schedules only
	for groupKey, winner := range winners {
		log.Info().
			Str("schedule", winner.sched.ID()).
			Str("group", groupKey).
			Time("prev_time", winner.prev.Time).
			Msg("Boot recovery: running most recent occurrence for group")

		// Use a boot-specific occurrence ID to avoid dedupe conflicts
		bootOcc := NewOccurrenceWithSuffix(winner.sched.ID(), now, "boot")
		s.emitDirect(winner.sched, bootOcc, "boot_recovery")
	}
}

// nextOccurrence finds the earliest next occurrence across all schedules
func (s *Scheduler) nextOccurrence(after time.Time) (*Occurrence, Schedule) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var earliest *Occurrence
	var source Schedule

	for _, sched := range s.schedules {
		if occ := sched.Next(after); occ != nil {
			if earliest == nil || occ.Time.Before(earliest.Time) {
				earliest = occ
				source = sched
			}
		}
	}

	return earliest, source
}

// emit publishes a schedule event to the bus with deduplication check
func (s *Scheduler) emit(sched Schedule, occ *Occurrence, source string) {
	// Deduplication check
	if s.ledger.HasCompleted(occ.ID) {
		log.Debug().Str("occurrence", occ.ID).Msg("Already completed, skipping")
		return
	}

	s.emitDirect(sched, occ, source)
}

// emitDirect publishes a schedule event without deduplication (for boot recovery)
func (s *Scheduler) emitDirect(sched Schedule, occ *Occurrence, source string) {
	log.Info().
		Str("schedule_id", sched.ID()).
		Str("occurrence_id", occ.ID).
		Str("action", sched.ActionName()).
		Time("time", occ.Time).
		Str("source", source).
		Msg("Emitting schedule event")

	s.bus.Publish(eventbus.Event{
		Type: eventbus.EventTypeSchedule,
		Data: map[string]interface{}{
			"schedule_id":   sched.ID(),
			"occurrence_id": occ.ID,
			"action_name":   sched.ActionName(),
			"action_args":   sched.ActionArgs(),
			"run_at":        occ.Time,
			"source":        source,
		},
	})
}

// RunClosest finds and executes the closest schedule matching criteria.
// Emits an event (no idempotency key for manual triggers).
func (s *Scheduler) RunClosest(tags []string, strategy Strategy) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	now := time.Now()
	var closest *Occurrence
	var closestSched Schedule

	for _, sched := range s.schedules {
		// Filter by tag
		if len(tags) > 0 && !containsTag(tags, sched.Tag()) {
			continue
		}

		var occ *Occurrence
		switch strategy {
		case StrategyNext:
			occ = sched.Next(now)
		case StrategyPrev:
			occ = sched.Prev(now)
		default:
			occ = sched.Next(now)
		}

		if occ == nil {
			continue
		}

		isCloser := closest == nil
		if !isCloser {
			switch strategy {
			case StrategyNext:
				isCloser = occ.Time.Before(closest.Time)
			case StrategyPrev:
				isCloser = occ.Time.After(closest.Time)
			}
		}

		if isCloser {
			closest = occ
			closestSched = sched
		}
	}

	if closestSched == nil {
		log.Warn().
			Strs("tags", tags).
			Str("strategy", string(strategy)).
			Msg("No matching schedule found")
		return
	}

	log.Info().
		Str("schedule_id", closestSched.ID()).
		Str("action", closestSched.ActionName()).
		Time("time", closest.Time).
		Str("strategy", string(strategy)).
		Msg("Executing closest schedule")

	// Use empty occurrence ID for manual triggers (no dedupe)
	manualOcc := &Occurrence{
		ID:         "", // No dedupe
		ScheduleID: closestSched.ID(),
		Time:       now,
	}
	s.emitDirect(closestSched, manualOcc, "run_closest")
}

func containsTag(tags []string, tag string) bool {
	for _, t := range tags {
		if t == tag {
			return true
		}
	}
	return false
}

// ScheduleEntry represents a single occurrence for display
type ScheduleEntry struct {
	ID         string
	TypeExpr   string
	Time       time.Time
	ActionName string
	Tag        string
	IsPast     bool
}

// FormatScheduleForDay returns a human-readable schedule for a specific day.
func (s *Scheduler) FormatScheduleForDay(day time.Time) string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if len(s.schedules) == 0 {
		return "No scheduled definitions"
	}

	now := time.Now().In(s.tz)
	dayInTz := day.In(s.tz)

	// Calculate day boundaries
	startOfDay := time.Date(dayInTz.Year(), dayInTz.Month(), dayInTz.Day(), 0, 0, 0, 0, s.tz)
	endOfDay := startOfDay.Add(24 * time.Hour)

	// Collect all occurrences for the day
	var entries []ScheduleEntry

	for _, sched := range s.schedules {
		typeExpr := s.getTypeExpr(sched)
		tag := sched.Tag()
		if tag == "" {
			tag = "-"
		}

		// For daily schedules, get today's occurrence
		if daily, ok := sched.(*DailySchedule); ok {
			occ := daily.Next(startOfDay.Add(-1 * time.Second))
			if occ != nil && occ.Time.Before(endOfDay) {
				entries = append(entries, ScheduleEntry{
					ID:         sched.ID(),
					TypeExpr:   typeExpr,
					Time:       occ.Time,
					ActionName: sched.ActionName(),
					Tag:        tag,
					IsPast:     occ.Time.Before(now),
				})
			}
		} else if periodic, ok := sched.(*PeriodicSchedule); ok {
			// For periodic schedules, collect ALL occurrences for today
			cursor := startOfDay.Add(-1 * time.Second)
			for {
				occ := periodic.Next(cursor)
				if occ == nil || !occ.Time.Before(endOfDay) {
					break
				}
				entries = append(entries, ScheduleEntry{
					ID:         sched.ID(),
					TypeExpr:   typeExpr,
					Time:       occ.Time,
					ActionName: sched.ActionName(),
					Tag:        tag,
					IsPast:     occ.Time.Before(now),
				})
				cursor = occ.Time
			}
		}
	}

	// Sort by time
	sortScheduleEntries(entries)

	// Format output
	var sb strings.Builder
	dateStr := dayInTz.Format("2006-01-02")
	sb.WriteString(fmt.Sprintf("Schedule for %s (timezone: %s)\n", dateStr, s.tz.String()))
	sb.WriteString(fmt.Sprintf("%-3s %-20s %-20s %-12s %-20s %s\n", "", "ID", "TYPE/EXPR", "TIME", "ACTION", "TAG"))
	sb.WriteString(strings.Repeat("-", 100) + "\n")

	for _, entry := range entries {
		status := " "
		if entry.IsPast {
			status = "✓"
		}

		timeStr := entry.Time.In(s.tz).Format("15:04:05")

		sb.WriteString(fmt.Sprintf("%-3s %-20s %-20s %-12s %-20s %s\n",
			status, entry.ID, entry.TypeExpr, timeStr, entry.ActionName, entry.Tag))
	}

	if len(entries) == 0 {
		sb.WriteString("No occurrences for this day\n")
	}

	return sb.String()
}

func (s *Scheduler) getTypeExpr(sched Schedule) string {
	switch v := sched.(type) {
	case *DailySchedule:
		return v.TimeExprString()
	case *PeriodicSchedule:
		return fmt.Sprintf("every %s", v.Interval())
	default:
		return "unknown"
	}
}

func sortScheduleEntries(entries []ScheduleEntry) {
	for i := 0; i < len(entries)-1; i++ {
		for j := i + 1; j < len(entries); j++ {
			if entries[j].Time.Before(entries[i].Time) {
				entries[i], entries[j] = entries[j], entries[i]
			}
		}
	}
}

// Timezone returns the scheduler's timezone
func (s *Scheduler) Timezone() *time.Location {
	return s.tz
}

// Evaluator returns the time expression evaluator (for creating schedules externally)
func (s *Scheduler) Evaluator() TimeEvaluator {
	return s.evaluator
}

// Disable removes a schedule by ID
func (s *Scheduler) Disable(id string) error {
	s.Unregister(id)
	return nil
}

// Enable is a no-op for in-memory schedules (schedule must be re-registered)
func (s *Scheduler) Enable(id string) error {
	log.Warn().Str("id", id).Msg("Enable called on in-memory scheduler - schedule must be re-registered via Lua")
	return nil
}
