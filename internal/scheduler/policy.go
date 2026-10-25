package scheduler

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/dokzlo13/lightd/internal/geo"
)

// PolicySpec is a pure, serializable calendar definition. No callbacks execute
// during evaluation, scheduling, or preview.
type PolicySpec struct {
	ID      string      `json:"id"`
	Version string      `json:"version"`
	Tag     string      `json:"tag,omitempty"`
	Phases  []PhaseSpec `json:"phases"`
}
type PhaseSpec struct {
	ID          string         `json:"id"`
	At          string         `json:"at,omitempty"`
	RelativeTo  string         `json:"relative_to,omitempty"`
	Offset      string         `json:"offset,omitempty"`
	OffsetCurve *OffsetCurve   `json:"offset_curve,omitempty"`
	Earliest    string         `json:"earliest,omitempty"`
	Latest      string         `json:"latest,omitempty"`
	Fallback    string         `json:"fallback,omitempty"`
	Action      string         `json:"action"`
	Args        map[string]any `json:"args,omitempty"`
}
type OffsetCurve struct {
	Input  string `json:"input"`
	From   string `json:"from"`
	To     string `json:"to"`
	Start  string `json:"start"`
	Finish string `json:"finish"`
	Easing string `json:"easing"`
}
type compiledPhase struct {
	spec                                        PhaseSpec
	anchor, earliest, latest, fallback          *TimeExpr
	offset                                      time.Duration
	curveFrom, curveTo, curveStart, curveFinish time.Duration
	displacement                                time.Duration
}
type Policy struct {
	spec      PolicySpec
	evaluator TimeEvaluator
	geo       *geo.Calculator
	phases    []compiledPhase
	byID      map[string]int
}
type PolicyPreview struct {
	ID       string                `json:"id"`
	Version  string                `json:"version"`
	Date     string                `json:"date"`
	Timezone string                `json:"timezone"`
	Solar    map[string]*time.Time `json:"solar,omitempty"`
	Phases   []PhasePreview        `json:"phases"`
}
type PhasePreview struct {
	ID          string     `json:"id"`
	Rank        int        `json:"rank"`
	Action      string     `json:"action"`
	Time        *time.Time `json:"time,omitempty"`
	Anchor      *time.Time `json:"anchor,omitempty"`
	CurveWeight *float64   `json:"curve_weight,omitempty"`
	Reasons     []string   `json:"reasons,omitempty"`
	Skipped     bool       `json:"skipped"`
}

// CompilePolicy validates structure and 366 consecutive local dates before a
// policy can replace a live definition. Evaluation remains validated at runtime.
func CompilePolicy(spec PolicySpec, calc *geo.Calculator, timezone string) (*Policy, error) {
	// Copy all nested argument maps so callers cannot mutate a live calendar.
	data, err := json.Marshal(spec)
	if err != nil {
		return nil, fmt.Errorf("policy data: %w", err)
	}
	if len(data) > maxPolicyBytes {
		return nil, fmt.Errorf("policy definition exceeds 1 MiB")
	}
	var copied PolicySpec
	if err = json.Unmarshal(data, &copied); err != nil {
		return nil, err
	}
	spec = copied
	if _, err := time.LoadLocation(timezone); err != nil {
		return nil, fmt.Errorf("policy timezone: %w", err)
	}
	if strings.TrimSpace(spec.ID) == "" || strings.TrimSpace(spec.Version) == "" {
		return nil, fmt.Errorf("policy id and version are required")
	}
	if len(spec.Phases) == 0 || len(spec.Phases) > 64 {
		return nil, fmt.Errorf("policy must contain 1 to 64 phases")
	}
	p := &Policy{spec: spec, geo: calc, byID: map[string]int{}}
	if calc != nil && calc.IsConfigured() {
		p.evaluator = NewAstroTimeEvaluator(calc, timezone)
	} else {
		p.evaluator = NewFixedTimeEvaluator(timezone)
	}
	for i, phase := range spec.Phases {
		if phase.ID == "" || phase.Action == "" {
			return nil, fmt.Errorf("phase %d needs id and action", i)
		}
		if _, exists := p.byID[phase.ID]; exists {
			return nil, fmt.Errorf("duplicate phase %q", phase.ID)
		}
		if (phase.At == "") == (phase.RelativeTo == "") {
			return nil, fmt.Errorf("phase %q needs exactly one of at or relative_to", phase.ID)
		}
		c := compiledPhase{spec: phase}
		var err error
		if phase.At != "" {
			c.anchor, err = ParseTimeExpr(phase.At)
			if err != nil {
				return nil, fmt.Errorf("phase %q: %w", phase.ID, err)
			}
			if c.anchor.IsAstronomical() && !p.evaluator.SupportsAstronomical() {
				return nil, fmt.Errorf("phase %q needs solar coordinates", phase.ID)
			}
		}
		if phase.Offset != "" {
			c.offset, err = boundedDuration(phase.Offset)
			if err != nil {
				return nil, fmt.Errorf("phase %q offset: %w", phase.ID, err)
			}
		}
		for _, bound := range []struct {
			raw  string
			dest **TimeExpr
			name string
		}{{phase.Earliest, &c.earliest, "earliest"}, {phase.Latest, &c.latest, "latest"}, {phase.Fallback, &c.fallback, "fallback"}} {
			if bound.raw == "" {
				continue
			}
			expr, err := ParseTimeExpr(bound.raw)
			if err != nil || !expr.IsFixed() {
				return nil, fmt.Errorf("phase %q %s must be HH:MM", phase.ID, bound.name)
			}
			*bound.dest = expr
		}
		if c.earliest != nil && c.latest != nil && c.earliest.FixedHour*60+c.earliest.FixedMin > c.latest.FixedHour*60+c.latest.FixedMin {
			return nil, fmt.Errorf("phase %q earliest exceeds latest", phase.ID)
		}
		if curve := phase.OffsetCurve; curve != nil {
			if curve.Input != "day_length" {
				return nil, fmt.Errorf("phase %q unsupported curve input %q", phase.ID, curve.Input)
			}
			if !p.evaluator.SupportsAstronomical() {
				return nil, fmt.Errorf("phase %q curve needs solar coordinates", phase.ID)
			}
			if curve.Easing != "smoothstep" && curve.Easing != "linear" {
				return nil, fmt.Errorf("phase %q curve easing must be linear or smoothstep", phase.ID)
			}
			vals := []*time.Duration{&c.curveFrom, &c.curveTo, &c.curveStart, &c.curveFinish}
			for j, raw := range []string{curve.From, curve.To, curve.Start, curve.Finish} {
				*vals[j], err = boundedDuration(raw)
				if err != nil {
					return nil, fmt.Errorf("phase %q curve: %w", phase.ID, err)
				}
			}
			if c.curveFrom == c.curveTo || c.curveFrom < 0 || c.curveTo < 0 {
				return nil, fmt.Errorf("phase %q curve endpoints must be distinct nonnegative day lengths", phase.ID)
			}
		}
		p.byID[phase.ID] = i
		p.phases = append(p.phases, c)
	}
	// Resolve dependencies and prove a maximum 48-hour displacement.
	seen := make([]uint8, len(p.phases))
	var visit func(int) (time.Duration, error)
	visit = func(i int) (time.Duration, error) {
		if seen[i] == 1 {
			return 0, fmt.Errorf("policy contains a phase reference cycle")
		}
		if seen[i] == 2 {
			return p.phases[i].displacement, nil
		}
		seen[i] = 1
		c := &p.phases[i]
		d := absDuration(c.offset)
		if c.anchor != nil {
			d += absDuration(c.anchor.Offset)
		}
		if c.spec.OffsetCurve != nil {
			d += max(absDuration(c.curveStart), absDuration(c.curveFinish))
		}
		if c.spec.RelativeTo != "" {
			ref, ok := p.byID[c.spec.RelativeTo]
			if !ok {
				return 0, fmt.Errorf("phase %q references missing phase %q", c.spec.ID, c.spec.RelativeTo)
			}
			rd, err := visit(ref)
			if err != nil {
				return 0, err
			}
			d += rd
		}
		if d > 48*time.Hour {
			return 0, fmt.Errorf("phase %q exceeds 48-hour total displacement", c.spec.ID)
		}
		c.displacement = d
		seen[i] = 2
		return d, nil
	}
	for i := range p.phases {
		if _, err := visit(i); err != nil {
			return nil, err
		}
	}
	start := time.Now().In(p.evaluator.Timezone())
	start = time.Date(start.Year(), start.Month(), start.Day(), 12, 0, 0, 0, p.evaluator.Timezone())
	for i := 0; i < 366; i++ {
		if _, err := p.previewDate(start.AddDate(0, 0, i)); err != nil {
			return nil, fmt.Errorf("policy validation: %w", err)
		}
	}
	return p, nil
}
func boundedDuration(raw string) (time.Duration, error) {
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, err
	}
	if d < -24*time.Hour || d > 24*time.Hour {
		return 0, fmt.Errorf("duration must be within 24 hours")
	}
	return d, nil
}
func absDuration(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}
func timePointer(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}
func (p *Policy) Spec() PolicySpec {
	data, _ := json.Marshal(p.spec)
	var copy PolicySpec
	_ = json.Unmarshal(data, &copy)
	return copy
}
func (p *Policy) Preview(date string) (PolicyPreview, error) {
	d, err := time.ParseInLocation("2006-01-02", date, p.evaluator.Timezone())
	if err != nil {
		return PolicyPreview{}, fmt.Errorf("date must be YYYY-MM-DD: %w", err)
	}
	return p.previewDate(d)
}
func (p *Policy) previewDate(date time.Time) (PolicyPreview, error) {
	result := PolicyPreview{ID: p.spec.ID, Version: p.spec.Version, Date: date.Format("2006-01-02"), Timezone: p.evaluator.Timezone().String(), Phases: make([]PhasePreview, len(p.phases))}
	var astro *geo.AstroTimes
	if p.geo != nil && p.geo.IsConfigured() {
		var err error
		astro, err = p.geo.GetTimes(date)
		if err != nil {
			return result, err
		}
		result.Solar = map[string]*time.Time{"dawn": timePointer(astro.Dawn), "sunrise": timePointer(astro.Sunrise), "noon": timePointer(astro.Noon), "sunset": timePointer(astro.Sunset), "dusk": timePointer(astro.Dusk)}
	}
	done := make([]bool, len(p.phases))
	var evaluate func(int) error
	evaluate = func(i int) error {
		if done[i] {
			return nil
		}
		c := p.phases[i]
		row := PhasePreview{ID: c.spec.ID, Rank: i, Action: c.spec.Action}
		var at time.Time
		var ok bool
		if c.anchor != nil {
			at, ok = p.evaluator.Evaluate(c.anchor, date)
			if !ok {
				row.Reasons = append(row.Reasons, "anchor unavailable (solar absence or nonexistent wall time)")
			}
		} else {
			ref := p.byID[c.spec.RelativeTo]
			if err := evaluate(ref); err != nil {
				return err
			}
			if result.Phases[ref].Time != nil {
				at = *result.Phases[ref].Time
				ok = true
			} else {
				row.Reasons = append(row.Reasons, "referenced phase unavailable")
			}
		}
		if ok {
			row.Anchor = timePointer(at)
			at = at.Add(c.offset)
		}
		if ok && c.spec.OffsetCurve != nil {
			if astro == nil || astro.Sunrise.IsZero() || astro.Sunset.IsZero() {
				ok = false
				row.Reasons = append(row.Reasons, "day length unavailable")
			} else {
				input := astro.Sunset.Sub(astro.Sunrise)
				u := float64(input-c.curveFrom) / float64(c.curveTo-c.curveFrom)
				u = math.Max(0, math.Min(1, u))
				if c.spec.OffsetCurve.Easing == "smoothstep" {
					u = u * u * (3 - 2*u)
				}
				row.CurveWeight = &u
				at = at.Add(c.curveStart + time.Duration(u*float64(c.curveFinish-c.curveStart)))
			}
		}
		if !ok && c.fallback != nil {
			at, ok = p.evaluator.Evaluate(c.fallback, date)
			row.Reasons = append(row.Reasons, "fixed fallback")
		}
		if ok && c.earliest != nil {
			bound, exists := p.evaluator.Evaluate(c.earliest, date)
			if !exists {
				ok = false
				row.Reasons = append(row.Reasons, "earliest bound is nonexistent wall time")
			} else if at.Before(bound) {
				at = bound
				row.Reasons = append(row.Reasons, "earliest clamp")
			}
		}
		if ok && c.latest != nil {
			bound, exists := p.evaluator.Evaluate(c.latest, date)
			if !exists {
				ok = false
				row.Reasons = append(row.Reasons, "latest bound is nonexistent wall time")
			} else if at.After(bound) {
				at = bound
				row.Reasons = append(row.Reasons, "latest clamp")
			}
		}
		if ok {
			at = at.Truncate(time.Second)
			row.Time = &at
		} else {
			row.Skipped = true
		}
		result.Phases[i] = row
		done[i] = true
		return nil
	}
	for i := range p.phases {
		if err := evaluate(i); err != nil {
			return result, err
		}
	}
	var last *time.Time
	for _, row := range result.Phases {
		if row.Time == nil {
			continue
		}
		if last != nil && row.Time.Before(*last) {
			return result, fmt.Errorf("date %s: phase %q precedes previous declared phase", result.Date, row.ID)
		}
		last = row.Time
	}
	return result, nil
}

// policySchedule uses the same immutable evaluator as offline preview.
type policySchedule struct {
	policy *Policy
	rank   int
}

func (s *policySchedule) ID() string { return s.policy.phases[s.rank].spec.ID }
func (s *policySchedule) Tag() string {
	if s.policy.spec.Tag != "" {
		return s.policy.spec.Tag
	}
	return s.policy.spec.ID
}
func (s *policySchedule) ActionName() string { return s.policy.phases[s.rank].spec.Action }
func (s *policySchedule) ActionArgs() map[string]any {
	args, _ := copyArgs(s.policy.phases[s.rank].spec.Args)
	return args
}
func (s *policySchedule) MisfirePolicy() MisfirePolicy      { return MisfirePolicyRunLatest }
func (s *policySchedule) Next(after time.Time) *Occurrence  { return s.find(after, true) }
func (s *policySchedule) Prev(before time.Time) *Occurrence { return s.find(before, false) }
func (s *policySchedule) find(query time.Time, next bool) *Occurrence {
	date := query.In(s.policy.evaluator.Timezone())
	date = time.Date(date.Year(), date.Month(), date.Day(), 12, 0, 0, 0, date.Location())
	var best *Occurrence
	for n := -4; n < 370; n++ {
		day := n
		if !next {
			day = -n
		}
		preview, err := s.policy.previewDate(date.AddDate(0, 0, day))
		if err != nil {
			continue
		}
		at := preview.Phases[s.rank].Time
		if at != nil && ((next && at.After(query)) || (!next && at.Before(query))) {
			if best == nil || (next && at.Before(best.Time)) || (!next && at.After(best.Time)) {
				best = NewOccurrence(s.ID(), *at)
			}
		}
		if best != nil && n > 4 {
			break
		}
	}
	return best
}

// A total order avoids non-transitive comparisons between policy ranks and
// unrelated schedule IDs. Within each policy the declared phase order wins.
func policyOrder(a, b Schedule) bool {
	key := func(s Schedule) (string, int, string) {
		if p, ok := s.(*policySchedule); ok {
			return "policy/" + p.policy.spec.ID, p.rank, s.ID()
		}
		return "schedule/" + s.ID(), 0, s.ID()
	}
	ak, ar, ai := key(a)
	bk, br, bi := key(b)
	if ak != bk {
		return ak < bk
	}
	if ar != br {
		return ar < br
	}
	return ai < bi
}
func sortOccurrences(items []scheduledOccurrence) {
	sort.Slice(items, func(i, j int) bool {
		if !items[i].occ.Time.Equal(items[j].occ.Time) {
			return items[i].occ.Time.Before(items[j].occ.Time)
		}
		return policyOrder(items[i].sched, items[j].sched)
	})
}
