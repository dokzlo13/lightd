package scheduler

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/dokzlo13/lightd/internal/events"
	"github.com/dokzlo13/lightd/internal/geo"
	"github.com/dokzlo13/lightd/internal/storage"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

type Strategy string

const (
	StrategyNext Strategy = "NEXT"
	StrategyPrev Strategy = "PREV"
)
const (
	catchupWindow     = 5 * time.Minute
	maxDueBatch       = 256
	maxPreviewEntries = 256
	maxSchedules      = 256
)

type Scheduler struct {
	mu             sync.RWMutex
	schedules      map[string]Schedule
	generations    map[string]int64
	nextGeneration int64
	policies       map[string][]string
	bus            *events.Bus
	ledger         *storage.Ledger
	evaluator      TimeEvaluator
	geo            *geo.Calculator
	tz             *time.Location
	reschedule     chan struct{}
	// cursors are owned by the scheduler goroutine (and boot recovery before Run).
	cursors map[string]time.Time
	now     func() time.Time
}

func New(bus *events.Bus, l *storage.Ledger, calc *geo.Calculator, timezone string) *Scheduler {
	s := NewWithFixedTimeOnly(bus, l, timezone)
	s.evaluator = NewAstroTimeEvaluator(calc, timezone)
	s.geo = calc
	return s
}
func NewWithFixedTimeOnly(bus *events.Bus, l *storage.Ledger, timezone string) *Scheduler {
	e := NewFixedTimeEvaluator(timezone)
	return &Scheduler{schedules: map[string]Schedule{}, generations: map[string]int64{}, policies: map[string][]string{}, bus: bus, ledger: l, evaluator: e, tz: e.Timezone(), reschedule: make(chan struct{}, 1), cursors: map[string]time.Time{}, now: time.Now}
}
func (s *Scheduler) Register(schedule Schedule) error {
	if schedule == nil || schedule.ID() == "" || schedule.ActionName() == "" {
		return fmt.Errorf("schedule id and action are required")
	}
	s.mu.Lock()
	if _, exists := s.schedules[schedule.ID()]; !exists && len(s.schedules) >= maxSchedules {
		s.mu.Unlock()
		return fmt.Errorf("at most %d schedule definitions are supported", maxSchedules)
	}
	for owner, ids := range s.policies {
		for _, id := range ids {
			if id == schedule.ID() {
				s.mu.Unlock()
				return fmt.Errorf("phase %q belongs to policy %q; replace the whole policy", id, owner)
			}
		}
	}
	s.nextGeneration++
	s.schedules[schedule.ID()] = schedule
	s.generations[schedule.ID()] = s.nextGeneration
	s.mu.Unlock()
	s.notifyReschedule()
	return nil
}
func (s *Scheduler) Unregister(id string) error { return s.Disable(id) }
func (s *Scheduler) Define(id, expr, action string, args map[string]any, tag string, misfire MisfirePolicy) error {
	schedule, err := NewDailySchedule(id, expr, action, args, tag, misfire, s.evaluator)
	if err != nil {
		return err
	}
	return s.Register(schedule)
}
func (s *Scheduler) DefinePeriodic(id string, interval time.Duration, action string, args map[string]any, tag string) error {
	if id == "" || action == "" {
		return fmt.Errorf("schedule id and action are required")
	}
	if interval < time.Second {
		return fmt.Errorf("periodic interval must be at least 1s")
	}
	copied, err := copyArgs(args)
	if err != nil {
		return fmt.Errorf("schedule arguments: %w", err)
	}
	return s.Register(NewPeriodicSchedule(id, interval, action, copied, tag))
}
func (s *Scheduler) InstallPolicy(spec PolicySpec) error {
	policy, err := CompilePolicy(spec, s.geo, s.tz.String())
	if err != nil {
		return err
	}
	s.mu.Lock()
	own := map[string]bool{}
	for _, id := range s.policies[spec.ID] {
		own[id] = true
	}
	if len(s.schedules)-len(own)+len(spec.Phases) > maxSchedules {
		s.mu.Unlock()
		return fmt.Errorf("at most %d schedule definitions are supported", maxSchedules)
	}
	for _, phase := range spec.Phases {
		if _, exists := s.schedules[phase.ID]; exists && !own[phase.ID] {
			s.mu.Unlock()
			return fmt.Errorf("phase id %q already belongs to another definition", phase.ID)
		}
	}
	for _, id := range s.policies[spec.ID] {
		delete(s.schedules, id)
		delete(s.generations, id)
	}
	s.nextGeneration++
	generation := s.nextGeneration
	ids := make([]string, 0, len(spec.Phases))
	for rank, phase := range spec.Phases {
		s.schedules[phase.ID] = &policySchedule{policy, rank}
		s.generations[phase.ID] = generation
		ids = append(ids, phase.ID)
	}
	s.policies[spec.ID] = ids
	s.mu.Unlock()
	s.notifyReschedule()
	return nil
}
func (s *Scheduler) PreviewPolicy(id, date string) (PolicyPreview, error) {
	s.mu.RLock()
	ids := s.policies[id]
	var p *Policy
	if len(ids) > 0 {
		if schedule, ok := s.schedules[ids[0]].(*policySchedule); ok {
			p = schedule.policy
		}
	}
	s.mu.RUnlock()
	if p == nil {
		return PolicyPreview{}, fmt.Errorf("policy %q not found", id)
	}
	return p.Preview(date)
}
func (s *Scheduler) ValidateActions(has func(string) bool) error {
	for _, item := range s.snapshot() {
		if !has(item.sched.ActionName()) {
			return fmt.Errorf("schedule %q references unknown action %q", item.sched.ID(), item.sched.ActionName())
		}
	}
	return nil
}
func (s *Scheduler) notifyReschedule() {
	select {
	case s.reschedule <- struct{}{}:
	default:
	}
}

type scheduledOccurrence struct {
	occ        *Occurrence
	sched      Schedule
	generation int64
}

func (s *Scheduler) snapshot() []scheduledOccurrence {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := make([]scheduledOccurrence, 0, len(s.schedules))
	for id, schedule := range s.schedules {
		items = append(items, scheduledOccurrence{sched: schedule, generation: s.generations[id]})
	}
	sort.Slice(items, func(i, j int) bool { return policyOrder(items[i].sched, items[j].sched) })
	return items
}
func (s *Scheduler) valid(id string, generation int64) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.generations[id] == generation && generation != 0
}
func revision(schedule Schedule) string {
	var v any
	switch t := schedule.(type) {
	case *policySchedule:
		v = t.policy.spec
	case *DailySchedule:
		v = []any{t.id, t.timeExpr.Raw, t.actionName, t.actionArgs, t.tag}
	case *PeriodicSchedule:
		v = []any{t.id, t.interval, t.actionName, t.actionArgs, t.tag}
	default:
		v = []any{schedule.ID(), schedule.ActionName(), schedule.ActionArgs()}
	}
	data, _ := json.Marshal(v)
	sum := sha256.Sum256(data)
	return fmt.Sprintf("%x", sum[:8])
}
func cursorKey(schedule Schedule) string { return schedule.ID() + "/" + revision(schedule) }
func (s *Scheduler) loadCursor(schedule Schedule, now time.Time) (time.Time, error) {
	key := cursorKey(schedule)
	if at, ok := s.cursors[key]; ok {
		return at, nil
	}
	if s.ledger != nil {
		at, ok, err := s.ledger.LoadCursor(key)
		if err != nil {
			return time.Time{}, err
		}
		if ok {
			s.cursors[key] = at
			return at, nil
		}
	}
	s.cursors[key] = now
	return now, nil
}
func (s *Scheduler) saveCursor(schedule Schedule, at time.Time) error {
	key := cursorKey(schedule)
	if old, ok := s.cursors[key]; ok && old.After(at) {
		at = old
	}
	if s.ledger != nil {
		if err := s.ledger.SaveCursor(key, at); err != nil {
			return err
		}
	}
	s.cursors[key] = at
	return nil
}

func (s *Scheduler) Run(ctx context.Context) error {
	log.Info().Msg("Scheduler started")
	// Capture the initial cursor before the first timer can expire.
	for _, item := range s.snapshot() {
		if _, err := s.loadCursor(item.sched, s.now()); err != nil {
			return err
		}
	}
	for {
		now := s.now()
		err := s.dispatchDue(ctx, now)
		delay := time.Hour
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			log.Error().Err(err).Msg("Schedule admission failed; retaining due occurrence")
			delay = time.Second
		} else {
			if occ, _ := s.nextPending(now); occ != nil {
				delay = occ.Time.Sub(now)
				if delay < 0 {
					delay = 0
				}
			}
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-s.reschedule:
			timer.Stop()
		case <-timer.C:
		}
	}
}
func (s *Scheduler) nextPending(now time.Time) (*Occurrence, Schedule) {
	var best *Occurrence
	var source Schedule
	for _, item := range s.snapshot() {
		cursor, err := s.loadCursor(item.sched, now)
		if err != nil {
			continue
		}
		occ := item.sched.Next(cursor)
		if occ != nil && (best == nil || occ.Time.Before(best.Time) || (occ.Time.Equal(best.Time) && policyOrder(item.sched, source))) {
			best = occ
			source = item.sched
		}
	}
	return best, source
}

// dispatchDue has an explicit clock input so delay, rejection, and catch-up
// behavior can be tested without wall-clock sleeps.
func (s *Scheduler) dispatchDue(ctx context.Context, now time.Time) error {
	var due []scheduledOccurrence
	policyMembers := map[string][]scheduledOccurrence{}
	policyCandidates := map[string][]scheduledOccurrence{}
	policyRemaining := map[string]int{}
	for _, item := range s.snapshot() {
		if err := ctx.Err(); err != nil {
			return err
		}
		cursor, err := s.loadCursor(item.sched, now)
		if err != nil {
			return err
		}
		if ps, ok := item.sched.(*policySchedule); ok {
			id := ps.policy.spec.ID
			policyMembers[id] = append(policyMembers[id], item)
			occ := item.sched.Prev(now.Add(time.Nanosecond))
			if occ == nil || !occ.Time.After(cursor) {
				continue
			}
			item.occ = occ
			policyCandidates[id] = append(policyCandidates[id], item)
			continue
		}
		cutoff := now.Add(-catchupWindow)
		if cursor.Before(cutoff) {
			if expired := item.sched.Prev(cutoff); expired != nil && expired.Time.After(cursor) {
				log.Warn().Str("schedule_id", item.sched.ID()).Time("through", expired.Time).Msg("Expired schedule occurrences outside five-minute catch-up window")
				if err = s.saveCursor(item.sched, expired.Time); err != nil {
					return err
				}
				cursor = expired.Time
			}
		}
		for count := 0; count < maxDueBatch; count++ {
			occ := item.sched.Next(cursor)
			if occ == nil || occ.Time.After(now) {
				break
			}
			if !occ.Time.After(cursor) {
				return fmt.Errorf("schedule %q did not advance", item.sched.ID())
			}
			due = append(due, scheduledOccurrence{occ, item.sched, item.generation})
			cursor = occ.Time
		}
	}
	for id, candidates := range policyCandidates {
		sortOccurrences(candidates)
		latest := candidates[len(candidates)-1].occ.Time
		for _, item := range candidates {
			if item.occ.Time.Equal(latest) {
				due = append(due, item)
				policyRemaining[id]++
			} else {
				log.Info().Str("policy_id", id).Str("schedule_id", item.sched.ID()).Time("planned_at", item.occ.Time).Msg("Superseded obsolete policy occurrence during catch-up")
			}
		}
	}
	sortOccurrences(due)
	if len(due) > maxDueBatch {
		due = due[:maxDueBatch]
	}
	for _, item := range due {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !s.valid(item.sched.ID(), item.generation) {
			continue
		}
		if err := s.emit(item, "scheduler", true); err != nil {
			return err
		}
		if err := s.saveCursor(item.sched, item.occ.Time); err != nil {
			return err
		}
		if ps, ok := item.sched.(*policySchedule); ok {
			id := ps.policy.spec.ID
			policyRemaining[id]--
			if policyRemaining[id] == 0 {
				for _, member := range policyMembers[id] {
					if err := s.saveCursor(member.sched, now); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}

// RunBootRecovery explicitly reasserts the latest phase per tag. Queued work
// from an old process is not replayed; the invoker records its interruption.
func (s *Scheduler) RunBootRecovery() error {
	now := s.now()
	items := s.snapshot()
	winners := map[string]scheduledOccurrence{}
	for _, item := range items {
		if item.sched.MisfirePolicy() == MisfirePolicySkip {
			continue
		}
		occ := item.sched.Prev(now.Add(time.Nanosecond))
		if occ == nil {
			continue
		}
		item.occ = occ
		key := item.sched.Tag()
		if key == "" {
			key = "__untagged:" + item.sched.ID()
		}
		prev, exists := winners[key]
		if !exists || occ.Time.After(prev.occ.Time) || (occ.Time.Equal(prev.occ.Time) && policyOrder(prev.sched, item.sched)) {
			winners[key] = item
		}
	}
	ordered := make([]scheduledOccurrence, 0, len(winners))
	for _, winner := range winners {
		ordered = append(ordered, winner)
	}
	sortOccurrences(ordered)
	for _, winner := range ordered {
		winner.occ = &Occurrence{ID: winner.occ.ID + "/boot/" + uuid.NewString(), ScheduleID: winner.occ.ScheduleID, Time: winner.occ.Time}
		if err := s.emit(winner, "boot_recovery", true); err != nil {
			return err
		}
	}
	for _, item := range items {
		if err := s.saveCursor(item.sched, now); err != nil {
			return err
		}
	}
	return nil
}
func (s *Scheduler) nextOccurrence(after time.Time) (*Occurrence, Schedule) {
	var best *Occurrence
	var source Schedule
	for _, item := range s.snapshot() {
		occ := item.sched.Next(after)
		if occ != nil && (best == nil || occ.Time.Before(best.Time) || (occ.Time.Equal(best.Time) && policyOrder(item.sched, source))) {
			best = occ
			source = item.sched
		}
	}
	return best, source
}
func (s *Scheduler) emit(item scheduledOccurrence, source string, dedupe bool, parents ...string) error {
	parent := ""
	if len(parents) > 0 {
		parent = parents[0]
	}
	if s.bus == nil {
		return fmt.Errorf("scheduler event bus unavailable")
	}
	key := ""
	if dedupe {
		key = item.occ.ID + "/" + revision(item.sched)
	}
	version := ""
	if ps, ok := item.sched.(*policySchedule); ok {
		version = ps.policy.spec.Version
	}
	return s.bus.Publish(events.Event{Type: events.EventTypeSchedule, Data: map[string]any{"parent_invocation_id": parent, "schedule_id": item.sched.ID(), "occurrence_id": key, "action_name": item.sched.ActionName(), "action_args": item.sched.ActionArgs(), "run_at": item.occ.Time, "source": source, "generation": item.generation, "policy_version": version, "generation_valid": func() bool { return s.valid(item.sched.ID(), item.generation) }}})
}
func validStrategy(strategy Strategy) bool {
	return strategy == StrategyNext || strategy == StrategyPrev
}
func (s *Scheduler) closest(tags []string, strategy Strategy) (scheduledOccurrence, error) {
	if !validStrategy(strategy) {
		return scheduledOccurrence{}, fmt.Errorf("strategy must be NEXT or PREV")
	}
	now := s.now()
	var best scheduledOccurrence
	for _, item := range s.snapshot() {
		if len(tags) > 0 && !containsTag(tags, item.sched.Tag()) {
			continue
		}
		if strategy == StrategyNext {
			item.occ = item.sched.Next(now)
		} else {
			item.occ = item.sched.Prev(now.Add(time.Nanosecond))
		}
		if item.occ == nil {
			continue
		}
		if best.occ == nil || (strategy == StrategyNext && item.occ.Time.Before(best.occ.Time)) || (strategy == StrategyPrev && item.occ.Time.After(best.occ.Time)) || (item.occ.Time.Equal(best.occ.Time) && ((strategy == StrategyNext && policyOrder(item.sched, best.sched)) || (strategy == StrategyPrev && policyOrder(best.sched, item.sched)))) {
			best = item
		}
	}
	if best.occ == nil {
		return best, fmt.Errorf("no matching schedule")
	}
	return best, nil
}
func (s *Scheduler) RunClosest(tags []string, strategy Strategy) error {
	return s.RunClosestWithParent(tags, strategy, "")
}
func (s *Scheduler) RunClosestWithParent(tags []string, strategy Strategy, parent string) error {
	item, err := s.closest(tags, strategy)
	if err != nil {
		return err
	}
	return s.emit(item, "run_closest", false, parent)
}

type ScheduleInfo struct {
	ID         string
	ActionName string
	Tag        string
	Time       time.Time
}

func (s *Scheduler) GetClosest(tags []string, strategy Strategy) *ScheduleInfo {
	item, err := s.closest(tags, strategy)
	if err != nil {
		return nil
	}
	return &ScheduleInfo{item.sched.ID(), item.sched.ActionName(), item.sched.Tag(), item.occ.Time}
}
func containsTag(tags []string, tag string) bool {
	for _, t := range tags {
		if t == tag {
			return true
		}
	}
	return false
}
func (s *Scheduler) GetSchedulesByTag(tag string) []ScheduleInfo {
	now := s.now().In(s.tz)
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, s.tz)
	var items []scheduledOccurrence
	for _, item := range s.snapshot() {
		if tag != "" && tag != item.sched.Tag() {
			continue
		}
		item.occ = item.sched.Next(start.Add(-time.Nanosecond))
		if item.occ != nil {
			items = append(items, item)
		}
	}
	sortOccurrences(items)
	result := make([]ScheduleInfo, 0, len(items))
	for _, item := range items {
		result = append(result, ScheduleInfo{item.sched.ID(), item.sched.ActionName(), item.sched.Tag(), item.occ.Time})
	}
	return result
}
func (s *Scheduler) RunByID(id string) error { return s.RunByIDWithParent(id, "") }
func (s *Scheduler) RunByIDWithParent(id, parent string) error {
	s.mu.RLock()
	schedule, ok := s.schedules[id]
	gen := s.generations[id]
	s.mu.RUnlock()
	if !ok {
		return fmt.Errorf("schedule %q not found", id)
	}
	return s.emit(scheduledOccurrence{&Occurrence{ScheduleID: id, Time: s.now()}, schedule, gen}, "run_by_id", false, parent)
}
func (s *Scheduler) Disable(id string) error {
	s.mu.Lock()
	if ids, ok := s.policies[id]; ok {
		for _, phase := range ids {
			delete(s.schedules, phase)
			delete(s.generations, phase)
		}
		delete(s.policies, id)
		s.mu.Unlock()
		s.notifyReschedule()
		return nil
	}
	for owner, ids := range s.policies {
		for _, phase := range ids {
			if phase == id {
				s.mu.Unlock()
				return fmt.Errorf("phase %q belongs to policy %q; disable the policy or replace its complete definition", id, owner)
			}
		}
	}
	_, exists := s.schedules[id]
	if exists {
		delete(s.schedules, id)
		delete(s.generations, id)
	}
	s.mu.Unlock()
	if !exists {
		return fmt.Errorf("schedule %q not found", id)
	}
	s.notifyReschedule()
	return nil
}
func (s *Scheduler) Enable(id string) error {
	return fmt.Errorf("schedule %q must be re-registered to enable it", id)
}
func (s *Scheduler) Timezone() *time.Location { return s.tz }
func (s *Scheduler) Evaluator() TimeEvaluator { return s.evaluator }

type ScheduleEntry struct {
	ID         string
	TypeExpr   string
	Time       time.Time
	ActionName string
	Tag        string
	IsPast     bool
}

func (s *Scheduler) FormatScheduleForDay(day time.Time) string {
	day = day.In(s.tz)
	start := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, s.tz)
	end := start.AddDate(0, 0, 1)
	var entries []scheduledOccurrence
	truncated := false
	for _, item := range s.snapshot() {
		cursor := start.Add(-time.Nanosecond)
		for n := 0; n < maxPreviewEntries; n++ {
			occ := item.sched.Next(cursor)
			if occ == nil || !occ.Time.Before(end) {
				break
			}
			if !occ.Time.After(cursor) {
				break
			}
			entries = append(entries, scheduledOccurrence{occ, item.sched, item.generation})
			cursor = occ.Time
			if n == maxPreviewEntries-1 {
				truncated = true
			}
		}
	}
	sortOccurrences(entries)
	if len(entries) > maxPreviewEntries {
		entries = entries[:maxPreviewEntries]
		truncated = true
	}
	var out strings.Builder
	fmt.Fprintf(&out, "Schedule for %s (timezone: %s)\n", day.Format("2006-01-02"), s.tz)
	for _, item := range entries {
		status := "future"
		if !item.occ.Time.After(s.now()) {
			status = "past"
		}
		fmt.Fprintf(&out, "%-6s %-24s %s %-20s %s\n", status, item.sched.ID(), item.occ.Time.In(s.tz).Format(time.RFC3339), item.sched.ActionName(), item.sched.Tag())
	}
	if len(entries) == 0 {
		out.WriteString("No occurrences for this day\n")
	}
	if truncated {
		fmt.Fprintf(&out, "Preview limited to %d entries; periodic schedules continue normally.\n", maxPreviewEntries)
	}
	out.WriteString("Past/future describes time, not action completion or physical light state.\n")
	return out.String()
}
