package scheduler

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/dokzlo13/lightd/internal/actions"
	"github.com/dokzlo13/lightd/internal/geo"
	"github.com/dokzlo13/lightd/internal/ledger"
)

// Strategy for finding closest schedule
type Strategy string

const (
	StrategyNext Strategy = "NEXT"
	StrategyPrev Strategy = "PREV"
)

// LuaInvokeFunc is a callback for invoking actions through the Lua worker
// This ensures Lua actions are executed in the single Lua worker goroutine
type LuaInvokeFunc func(ctx context.Context, actionName string, args map[string]any, idempotencyKey, source, defID string) error

// Scheduler manages schedule definitions and occurrence execution
type Scheduler struct {
	defStore  *DefinitionStore
	occStore  *OccurrenceStore
	invoker   *actions.Invoker
	ledger    *ledger.Ledger
	evaluator *TimeExprEvaluator
	geo       *geo.Calculator
	location  string
	timezone  string
	tz        *time.Location

	mu         sync.RWMutex
	reschedule chan struct{}

	// luaInvoke routes action invocations through the Lua worker for thread safety
	// If nil, falls back to direct invoker calls (unsafe for Lua actions)
	luaInvoke LuaInvokeFunc
}

// New creates a new scheduler
func New(
	defStore *DefinitionStore,
	occStore *OccurrenceStore,
	invoker *actions.Invoker,
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
		defStore:   defStore,
		occStore:   occStore,
		invoker:    invoker,
		ledger:     l,
		evaluator:  NewTimeExprEvaluator(geoCalc, location, timezone),
		geo:        geoCalc,
		location:   location,
		timezone:   timezone,
		tz:         tz,
		reschedule: make(chan struct{}, 1),
	}
}

// Define registers a schedule definition
func (s *Scheduler) Define(id, timeExpr, actionName string, args map[string]any, tag string, misfirePolicy MisfirePolicy) error {
	// Validate time expression
	if _, err := ParseTimeExpr(timeExpr); err != nil {
		return fmt.Errorf("invalid time expression: %w", err)
	}

	def := &Definition{
		ID:            id,
		TimeExpr:      timeExpr,
		ActionName:    actionName,
		ActionArgs:    args,
		Tag:           tag,
		MisfirePolicy: misfirePolicy,
		Enabled:       true,
		CreatedAt:     time.Now().UTC(),
	}

	if err := s.defStore.Upsert(def); err != nil {
		return fmt.Errorf("failed to save definition: %w", err)
	}

	log.Debug().
		Str("id", id).
		Str("time_expr", timeExpr).
		Str("action", actionName).
		Str("tag", tag).
		Msg("Schedule definition registered")

	// Recompute occurrences
	s.notifyReschedule()

	return nil
}

// notifyReschedule signals the scheduler to recalculate
func (s *Scheduler) notifyReschedule() {
	select {
	case s.reschedule <- struct{}{}:
	default:
	}
}

// SetLuaInvoker sets the callback for invoking actions through the Lua worker
// This MUST be called before Run() to ensure thread-safe Lua action execution
func (s *Scheduler) SetLuaInvoker(fn LuaInvokeFunc) {
	s.luaInvoke = fn
}

// ComputeOccurrences computes the next occurrence for all enabled definitions
func (s *Scheduler) ComputeOccurrences() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	defs, err := s.defStore.GetAllEnabled()
	if err != nil {
		return fmt.Errorf("failed to get definitions: %w", err)
	}

	now := time.Now()

	for _, def := range defs {
		// Clear existing occurrences for this definition
		if err := s.occStore.Clear(def.ID); err != nil {
			log.Error().Err(err).Str("def_id", def.ID).Msg("Failed to clear occurrences")
			continue
		}

		expr, err := def.ParsedTimeExpr()
		if err != nil {
			log.Error().Err(err).Str("def_id", def.ID).Msg("Failed to parse time expression")
			continue
		}

		// Find next occurrence
		nextTime, ok := s.evaluator.ComputeNextOccurrence(expr, now)
		if !ok {
			log.Warn().Str("def_id", def.ID).Msg("No next occurrence found")
			continue
		}

		occ := &Occurrence{
			DefID:        def.ID,
			OccurrenceID: OccurrenceID(def.ID, nextTime),
			RunAt:        nextTime,
			IsNext:       true,
		}

		if err := s.occStore.Insert(occ); err != nil {
			log.Error().Err(err).Str("def_id", def.ID).Msg("Failed to insert occurrence")
		}
	}

	return nil
}

// HandleMisfires processes missed occurrences on startup
func (s *Scheduler) HandleMisfires(ctx context.Context) error {
	defs, err := s.defStore.GetAllEnabled()
	if err != nil {
		return fmt.Errorf("failed to get definitions: %w", err)
	}

	now := time.Now()

	for _, def := range defs {
		// Get last completed time for this specific definition
		lastCompleted, hasLast := s.ledger.GetLastCompletedForDef(def.ID)

		expr, err := def.ParsedTimeExpr()
		if err != nil {
			continue
		}

		// Compute missed occurrences since last completed
		missed := s.computeMissedOccurrences(def, expr, lastCompleted, hasLast, now)
		if len(missed) == 0 {
			continue
		}

		log.Info().
			Str("def_id", def.ID).
			Int("missed_count", len(missed)).
			Str("policy", string(def.MisfirePolicy)).
			Msg("Processing missed occurrences")

		switch def.MisfirePolicy {
		case MisfirePolicySkip:
			// Do nothing

		case MisfirePolicyRunLatest:
			// Run only the most recent missed occurrence
			latest := missed[len(missed)-1]
			occID := OccurrenceID(def.ID, latest)
			if !s.ledger.HasCompleted(occID) {
				s.invokeScheduled(ctx, def, latest, occID)
			}

		case MisfirePolicyRunAll:
			// Run all missed in order
			for _, t := range missed {
				occID := OccurrenceID(def.ID, t)
				if !s.ledger.HasCompleted(occID) {
					s.invokeScheduled(ctx, def, t, occID)
				}
			}
		}
	}

	return nil
}

func (s *Scheduler) computeMissedOccurrences(def *Definition, expr *TimeExpr, lastCompleted time.Time, hasLast bool, now time.Time) []time.Time {
	var missed []time.Time

	// If no last completed, don't try to compute missed (fresh start)
	if !hasLast {
		return nil
	}

	// Walk from last completed to now
	current := lastCompleted
	for i := 0; i < 366; i++ { // Safety limit
		nextTime, ok := s.evaluator.ComputeNextOccurrence(expr, current)
		if !ok {
			break
		}

		if nextTime.After(now) {
			break
		}

		missed = append(missed, nextTime)
		current = nextTime
	}

	return missed
}

// invokeScheduled invokes a scheduled action with occurrence tracking
// Uses luaInvoke if set to ensure Lua actions run in the Lua worker goroutine
func (s *Scheduler) invokeScheduled(ctx context.Context, def *Definition, runAt time.Time, occurrenceID string) {
	log.Info().
		Str("def_id", def.ID).
		Str("action", def.ActionName).
		Time("run_at", runAt).
		Str("occurrence_id", occurrenceID).
		Msg("Invoking scheduled action")

	var err error
	if s.luaInvoke != nil {
		// Route through Lua worker for thread-safe execution
		err = s.luaInvoke(ctx, def.ActionName, def.ActionArgs, occurrenceID, "scheduler", def.ID)
	} else {
		// Direct invocation (only safe for non-Lua actions)
		err = s.invoker.InvokeWithSource(ctx, def.ActionName, def.ActionArgs, occurrenceID, "scheduler", def.ID)
	}

	if err != nil {
		log.Error().Err(err).
			Str("def_id", def.ID).
			Str("occurrence_id", occurrenceID).
			Msg("Failed to invoke scheduled action")
	}
}

// RunClosest finds and executes the closest definition matching criteria
// Uses NO idempotency key for manual/programmatic calls (always runs)
func (s *Scheduler) RunClosest(ctx context.Context, tags []string, strategy Strategy) error {
	defs, err := s.getDefinitionsByTags(tags)
	if err != nil {
		return err
	}

	if len(defs) == 0 {
		log.Warn().Strs("tags", tags).Msg("No definitions found with specified tags")
		return nil
	}

	now := time.Now()
	var closestDef *Definition
	var closestTime time.Time

	for _, def := range defs {
		expr, err := def.ParsedTimeExpr()
		if err != nil {
			continue
		}

		var t time.Time
		var ok bool

		switch strategy {
		case StrategyNext:
			t, ok = s.evaluator.ComputeNextOccurrence(expr, now)
		case StrategyPrev:
			t, ok = s.evaluator.ComputePrevOccurrence(expr, now)
		default:
			t, ok = s.evaluator.ComputeNextOccurrence(expr, now)
		}

		if !ok {
			continue
		}

		isCloser := closestDef == nil
		if !isCloser {
			switch strategy {
			case StrategyNext:
				isCloser = t.Before(closestTime)
			case StrategyPrev:
				isCloser = t.After(closestTime)
			}
		}

		if isCloser {
			closestDef = def
			closestTime = t
		}
	}

	if closestDef == nil {
		log.Warn().
			Strs("tags", tags).
			Str("strategy", string(strategy)).
			Msg("No matching definition found")
		return nil
	}

	log.Info().
		Str("def_id", closestDef.ID).
		Str("action", closestDef.ActionName).
		Time("time", closestTime).
		Str("strategy", string(strategy)).
		Msg("Executing closest definition")

	// Use empty idempotency key for manual calls (always run, no dedupe)
	return s.invoker.Invoke(ctx, closestDef.ActionName, closestDef.ActionArgs, "")
}

func (s *Scheduler) getDefinitionsByTags(tags []string) ([]*Definition, error) {
	if len(tags) == 0 {
		return s.defStore.GetAllEnabled()
	}

	var allDefs []*Definition
	seen := make(map[string]bool)

	for _, tag := range tags {
		defs, err := s.defStore.GetByTag(tag)
		if err != nil {
			return nil, err
		}
		for _, def := range defs {
			if !seen[def.ID] {
				seen[def.ID] = true
				allDefs = append(allDefs, def)
			}
		}
	}

	return allDefs, nil
}

// Run starts the scheduler loop
func (s *Scheduler) Run(ctx context.Context) error {
	// Initial occurrence computation
	if err := s.ComputeOccurrences(); err != nil {
		log.Error().Err(err).Msg("Failed to compute initial occurrences")
	}

	// Handle misfires on startup
	if err := s.HandleMisfires(ctx); err != nil {
		log.Error().Err(err).Msg("Failed to handle misfires")
	}

	log.Info().Msg("Scheduler started")

	for {
		nextWakeup := s.nextWakeupTime()
		sleepDuration := time.Until(nextWakeup)
		if sleepDuration < 0 {
			sleepDuration = 0
		}

		log.Debug().
			Time("next_wakeup", nextWakeup).
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
			if err := s.ComputeOccurrences(); err != nil {
				log.Error().Err(err).Msg("Failed to recompute occurrences")
			}
			continue

		case <-timer.C:
			s.executeReadyOccurrences(ctx)
		}
	}
}

func (s *Scheduler) nextWakeupTime() time.Time {
	now := time.Now()

	occ, err := s.occStore.GetNext()
	if err != nil || occ == nil {
		return now.Add(time.Hour) // Default wakeup
	}

	if occ.RunAt.Before(now) {
		return now
	}

	return occ.RunAt
}

func (s *Scheduler) executeReadyOccurrences(ctx context.Context) {
	now := time.Now()

	occs, err := s.occStore.GetPending(now)
	if err != nil {
		log.Error().Err(err).Msg("Failed to get pending occurrences")
		return
	}

	for _, occ := range occs {
		def, err := s.defStore.Get(occ.DefID)
		if err != nil || def == nil {
			log.Error().Err(err).Str("def_id", occ.DefID).Msg("Definition not found")
			s.occStore.MarkProcessed(occ.OccurrenceID)
			continue
		}

		s.invokeScheduled(ctx, def, occ.RunAt, occ.OccurrenceID)
		s.occStore.MarkProcessed(occ.OccurrenceID)

		// Compute next occurrence for this definition
		s.computeNextForDef(def)
	}
}

func (s *Scheduler) computeNextForDef(def *Definition) {
	expr, err := def.ParsedTimeExpr()
	if err != nil {
		return
	}

	nextTime, ok := s.evaluator.ComputeNextOccurrence(expr, time.Now())
	if !ok {
		return
	}

	occ := &Occurrence{
		DefID:        def.ID,
		OccurrenceID: OccurrenceID(def.ID, nextTime),
		RunAt:        nextTime,
		IsNext:       true,
	}

	if err := s.occStore.Insert(occ); err != nil {
		log.Error().Err(err).Str("def_id", def.ID).Msg("Failed to insert next occurrence")
	}
}

// FormatSchedule returns a human-readable schedule
func (s *Scheduler) FormatSchedule() string {
	defs, err := s.defStore.GetAllEnabled()
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}

	if len(defs) == 0 {
		return "No scheduled definitions"
	}

	now := time.Now()
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%-20s %-20s %-25s %-20s %s\n", "ID", "TIME EXPR", "NEXT RUN", "ACTION", "TAG"))
	sb.WriteString(strings.Repeat("-", 110) + "\n")

	for _, def := range defs {
		expr, _ := def.ParsedTimeExpr()
		nextTime, ok := s.evaluator.ComputeNextOccurrence(expr, now)

		nextRunStr := "-"
		if ok {
			nextRunStr = nextTime.In(s.tz).Format("2006-01-02 15:04:05")
		}

		tag := def.Tag
		if tag == "" {
			tag = "-"
		}

		sb.WriteString(fmt.Sprintf("%-20s %-20s %-25s %-20s %s\n",
			def.ID, def.TimeExpr, nextRunStr, def.ActionName, tag))
	}

	return sb.String()
}

// Timezone returns the scheduler's timezone
func (s *Scheduler) Timezone() *time.Location {
	return s.tz
}

// Disable disables a definition
func (s *Scheduler) Disable(id string) error {
	if err := s.defStore.SetEnabled(id, false); err != nil {
		return err
	}
	s.occStore.Clear(id)
	s.notifyReschedule()
	return nil
}

// Enable enables a definition
func (s *Scheduler) Enable(id string) error {
	if err := s.defStore.SetEnabled(id, true); err != nil {
		return err
	}
	s.notifyReschedule()
	return nil
}
