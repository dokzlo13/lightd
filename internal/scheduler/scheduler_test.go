package scheduler

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/dokzlo13/lightd/internal/events"
	"github.com/dokzlo13/lightd/internal/geo"
	"github.com/dokzlo13/lightd/internal/storage"
)

func zone(t *testing.T) *time.Location {
	t.Helper()
	tz, err := time.LoadLocation("Europe/Helsinki")
	if err != nil {
		t.Fatal(err)
	}
	return tz
}
func mustDefine(t *testing.T, s *Scheduler, id, at string) {
	t.Helper()
	if err := s.Define(id, at, "noop", nil, "", MisfirePolicyRunLatest); err != nil {
		t.Fatal(err)
	}
}
func TestDSTCivilDatesAndWallTimeRules(t *testing.T) {
	tz := zone(t)
	s := NewWithFixedTimeOnly(nil, nil, tz.String())
	mustDefine(t, s, "late", "23:50")
	autumn := time.Date(2026, 10, 25, 12, 0, 0, 0, tz)
	if !strings.Contains(s.FormatScheduleForDay(autumn), "23:50:00+02:00") {
		t.Fatal("25-hour day lost last hour")
	}
	spring := time.Date(2026, 3, 29, 0, 0, 0, 0, tz)
	p := NewPeriodicSchedule("tick", 30*time.Minute, "noop", nil, "")
	p.startTime = spring
	s.Register(p)
	if n := strings.Count(s.FormatScheduleForDay(spring), "tick"); n != 46 {
		t.Fatalf("spring day has %d half-hour ticks", n)
	}
	expr, _ := ParseTimeExpr("03:30")
	e := s.Evaluator()
	if _, ok := e.Evaluate(expr, spring); ok {
		t.Fatal("DST gap must be absent")
	}
	at, ok := e.Evaluate(expr, autumn)
	if !ok || at.Format(time.RFC3339) != "2026-10-25T03:30:00+03:00" {
		t.Fatalf("fold selected %v", at)
	}
	next, ok := e.ComputeNextOccurrence(expr, time.Date(2026, 3, 28, 23, 0, 0, 0, tz))
	if !ok || next.Day() != 30 {
		t.Fatalf("next gap result %v", next)
	}
}
func TestSolarOffsetsAcrossMidnight(t *testing.T) {
	tz := zone(t)
	calc := geo.NewCalculator(&geo.Location{Latitude: 60.17, Longitude: 24.94, Timezone: tz.String()})
	e := NewAstroTimeEvaluator(calc, tz.String())
	for _, test := range []struct {
		expr        string
		query       time.Time
		anchorDelta int
		next        bool
	}{{"@sunset + 10h", time.Date(2026, 11, 2, 0, 30, 0, 0, tz), -1, true}, {"@dawn - 8h", time.Date(2026, 11, 2, 23, 30, 0, 0, tz), 1, false}} {
		expr, err := ParseTimeExpr(test.expr)
		if err != nil {
			t.Fatal(err)
		}
		want, ok := e.Evaluate(expr, test.query.AddDate(0, 0, test.anchorDelta))
		if !ok {
			t.Fatal("fixture absent")
		}
		var got time.Time
		if test.next {
			got, ok = e.ComputeNextOccurrence(expr, test.query)
		} else {
			got, ok = e.ComputePrevOccurrence(expr, test.query)
		}
		if !ok || !got.Equal(want) {
			t.Fatalf("%s got %v want %v", test.expr, got, want)
		}
	}
}
func TestDueBatchAdmissionAndPersistentCursor(t *testing.T) {
	db, err := storage.Open(t.TempDir() + "/state.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ledger := storage.NewLedger(db.DB)
	bus := events.NewBus()
	defer bus.Close(context.Background())
	rejected := true
	var ids []string
	bus.SubscribeAck(events.EventTypeSchedule, func(e events.Event) error {
		if rejected {
			return events.ErrQueueFull
		}
		ids = append(ids, e.Data["schedule_id"].(string))
		return nil
	})
	s := NewWithFixedTimeOnly(bus, ledger, "UTC")
	mustDefine(t, s, "b", "12:00")
	mustDefine(t, s, "a", "12:00")
	mustDefine(t, s, "c", "12:01")
	before := time.Date(2026, 10, 25, 11, 59, 0, 0, time.UTC)
	now := before.Add(3 * time.Minute)
	for _, item := range s.snapshot() {
		if err := s.saveCursor(item.sched, before); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.dispatchDue(context.Background(), now); !errors.Is(err, events.ErrQueueFull) {
		t.Fatalf("rejection=%v", err)
	}
	at, _, err := ledger.LoadCursor(cursorKey(s.schedules["a"]))
	if err != nil || !at.Equal(before) {
		t.Fatal("cursor advanced over rejection", at, err)
	}
	rejected = false
	if err := s.dispatchDue(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	if strings.Join(ids, ",") != "a,b,c" {
		t.Fatalf("due set/order=%v", ids)
	}
	if err := s.dispatchDue(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	if len(ids) != 3 {
		t.Fatal("occurrences repeated")
	}
	// A new scheduler with the same definitions retains admitted progress.
	restarted := NewWithFixedTimeOnly(bus, ledger, "UTC")
	mustDefine(t, restarted, "a", "12:00")
	if err := restarted.dispatchDue(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	if len(ids) != 3 {
		t.Fatal("persisted cursor ignored")
	}
}
func TestBoundedCatchupExpiresOldAndKeepsEligible(t *testing.T) {
	bus := events.NewBus()
	defer bus.Close(context.Background())
	count := 0
	bus.SubscribeAck(events.EventTypeSchedule, func(events.Event) error { count++; return nil })
	s := NewWithFixedTimeOnly(bus, nil, "UTC")
	p := NewPeriodicSchedule("tick", time.Second, "noop", nil, "")
	p.startTime = time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	s.Register(p)
	_ = s.saveCursor(p, p.startTime)
	now := p.startTime.Add(10 * time.Minute)
	if err := s.dispatchDue(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	if count != maxDueBatch {
		t.Fatalf("batch=%d", count)
	}
	if err := s.dispatchDue(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	if count != 301 {
		t.Fatalf("eligible boundary ticks=%d", count)
	}
	if err := s.dispatchDue(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	if count != 301 {
		t.Fatal("replayed catchup")
	}
}
func TestPolicyCatchupOnlyLatestAndGenerationGuard(t *testing.T) {
	bus := events.NewBus()
	defer bus.Close(context.Background())
	var delivered []events.Event
	bus.SubscribeAck(events.EventTypeSchedule, func(e events.Event) error { delivered = append(delivered, e); return nil })
	s := NewWithFixedTimeOnly(bus, nil, "UTC")
	spec := PolicySpec{ID: "phases", Version: "1", Phases: []PhaseSpec{{ID: "morning", At: "08:00", Action: "noop"}, {ID: "evening", At: "18:00", Action: "noop"}, {ID: "night", At: "22:00", Action: "noop"}}}
	if err := s.InstallPolicy(spec); err != nil {
		t.Fatal(err)
	}
	before := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	for _, item := range s.snapshot() {
		_ = s.saveCursor(item.sched, before)
	}
	now := time.Date(2026, 10, 25, 23, 0, 0, 0, time.UTC)
	if err := s.dispatchDue(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	if len(delivered) != 1 || delivered[0].Data["schedule_id"] != "night" {
		t.Fatalf("policy catch-up=%v", delivered)
	}
	valid := delivered[0].Data["generation_valid"].(func() bool)
	if !valid() {
		t.Fatal("fresh generation invalid")
	}
	spec.Version = "2"
	if err := s.InstallPolicy(spec); err != nil {
		t.Fatal(err)
	}
	if valid() {
		t.Fatal("replaced queued work remains valid")
	}
}
func TestBootKeepsPlannedTimeAndManualRepeatability(t *testing.T) {
	bus := events.NewBus()
	defer bus.Close(context.Background())
	var delivered []events.Event
	bus.SubscribeAck(events.EventTypeSchedule, func(e events.Event) error { delivered = append(delivered, e); return nil })
	s := NewWithFixedTimeOnly(bus, nil, "UTC")
	now := time.Date(2026, 10, 25, 15, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	mustDefine(t, s, "noon", "12:00")
	if err := s.RunBootRecovery(); err != nil {
		t.Fatal(err)
	}
	if got := delivered[0].Data["run_at"].(time.Time); got.Hour() != 12 {
		t.Fatal("boot replaced planned time", got)
	}
	if err := s.RunByID("noon"); err != nil {
		t.Fatal(err)
	}
	if err := s.RunByID("noon"); err != nil {
		t.Fatal(err)
	}
	if len(delivered) != 3 || delivered[1].Data["occurrence_id"] != "" || delivered[2].Data["occurrence_id"] != "" {
		t.Fatal("manual calls deduplicated")
	}
}
func TestInvalidDefinitionsAndBoundedPreview(t *testing.T) {
	s := NewWithFixedTimeOnly(nil, nil, "UTC")
	for _, d := range []time.Duration{0, -time.Second, time.Millisecond} {
		if err := s.DefinePeriodic("bad", d, "noop", nil, ""); err == nil {
			t.Fatal("invalid interval accepted", d)
		}
	}
	if err := s.Define("", "12:00", "noop", nil, "", MisfirePolicySkip); err == nil {
		t.Fatal("empty ID accepted")
	}
	if err := s.RunClosest(nil, "SIDEWAYS"); err == nil {
		t.Fatal("invalid strategy accepted")
	}
	if err := s.RunClosest(nil, StrategyNext); err == nil {
		t.Fatal("missing closest returned success")
	}
	p := NewPeriodicSchedule("tick", time.Second, "noop", nil, "")
	p.startTime = time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC)
	s.Register(p)
	printed := s.FormatScheduleForDay(p.startTime)
	if strings.Count(printed, "tick") != 256 || !strings.Contains(printed, "limited to 256") {
		t.Fatal("preview not bounded")
	}
}
func TestListUsesSchedulerDate(t *testing.T) {
	s := NewWithFixedTimeOnly(nil, nil, "Pacific/Kiritimati")
	s.now = func() time.Time { return time.Date(2026, 10, 25, 23, 0, 0, 0, time.UTC) }
	mustDefine(t, s, "noon", "12:00")
	items := s.GetSchedulesByTag("")
	if len(items) != 1 || items[0].Time.Format("2006-01-02") != "2026-10-26" {
		t.Fatalf("wrong calendar date: %v", items)
	}
}

func TestPolicyEqualDeadlineRejectionDoesNotSkipSibling(t *testing.T) {
	bus := events.NewBus()
	defer bus.Close(context.Background())
	var ids []string
	reject := true
	bus.SubscribeAck(events.EventTypeSchedule, func(e events.Event) error {
		id := e.Data["schedule_id"].(string)
		if id == "b" && reject {
			return events.ErrQueueFull
		}
		ids = append(ids, id)
		return nil
	})
	s := NewWithFixedTimeOnly(bus, nil, "UTC")
	spec := PolicySpec{ID: "p", Version: "1", Phases: []PhaseSpec{{ID: "a", At: "12:00", Action: "noop"}, {ID: "b", At: "12:00", Action: "noop"}}}
	if err := s.InstallPolicy(spec); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	for _, item := range s.snapshot() {
		_ = s.saveCursor(item.sched, now.Add(-time.Second))
	}
	if err := s.dispatchDue(context.Background(), now); !errors.Is(err, events.ErrQueueFull) {
		t.Fatal(err)
	}
	reject = false
	if err := s.dispatchDue(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	if strings.Join(ids, ",") != "a,b" {
		t.Fatalf("equal policy siblings: %v", ids)
	}
}
