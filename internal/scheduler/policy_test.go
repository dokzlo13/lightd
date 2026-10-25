package scheduler

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dokzlo13/lightd/internal/geo"
)

func TestSeasonalPolicyAnnualParityAndBounds(t *testing.T) {
	spec, err := LoadPolicyFile(context.Background(), "../../examples/seasonal-policy.lua")
	if err != nil {
		t.Fatal(err)
	}
	tz := zone(t)
	calc := geo.NewCalculator(&geo.Location{Latitude: 60.17, Longitude: 24.94, Timezone: tz.String()})
	p, err := CompilePolicy(spec, calc, tz.String())
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 1, 1, 12, 0, 0, 0, tz)
	for d := 0; d < 366; d++ {
		date := start.AddDate(0, 0, d)
		preview, err := p.Preview(date.Format("2006-01-02"))
		if err != nil {
			t.Fatal(err)
		}
		if len(preview.Phases) != 6 {
			t.Fatal("missing phase")
		}
		warm := preview.Phases[3].Time
		if warm == nil {
			t.Fatal("Helsinki warm phase absent")
		}
		minutes := warm.Hour()*60 + warm.Minute()
		if minutes < 17*60 || minutes > 20*60+30 {
			t.Fatalf("warm out of bounds: %v", warm)
		}
		if preview.Phases[4].Time.Format("15:04") != "22:45" || preview.Phases[5].Time.Format("15:04") != "23:50" {
			t.Fatal("bedtime changed")
		}
		for i, phase := range preview.Phases {
			if phase.Time == nil {
				continue
			}
			schedule := &policySchedule{p, i}
			next := schedule.Next(phase.Time.Add(-time.Second))
			prev := schedule.Prev(phase.Time.Add(time.Second))
			if next == nil || prev == nil || !next.Time.Equal(*phase.Time) || !prev.Time.Equal(*phase.Time) {
				t.Fatalf("date %s phase %s preview/search disagree", preview.Date, phase.ID)
			}
		}
	}
}
func TestPolicyRejectsInvalidAndReplacementIsAtomic(t *testing.T) {
	valid := PolicySpec{ID: "p", Version: "1", Phases: []PhaseSpec{{ID: "a", At: "12:00", Action: "noop"}, {ID: "b", At: "13:00", Action: "noop"}}}
	s := NewWithFixedTimeOnly(nil, nil, "UTC")
	if err := s.InstallPolicy(valid); err != nil {
		t.Fatal(err)
	}
	bad := valid
	bad.Phases = []PhaseSpec{{ID: "a", RelativeTo: "b", Action: "noop"}, {ID: "b", RelativeTo: "a", Action: "noop"}}
	if err := s.InstallPolicy(bad); err == nil {
		t.Fatal("cycle accepted")
	}
	preview, err := s.PreviewPolicy("p", "2026-10-25")
	if err != nil || preview.Phases[0].Time.Hour() != 12 {
		t.Fatal("failed replacement changed policy", err)
	}
	cases := []PolicySpec{
		{ID: "p", Version: "1", Phases: []PhaseSpec{{ID: "a", RelativeTo: "missing", Action: "noop"}}},
		{ID: "p", Version: "1", Phases: []PhaseSpec{{ID: "a", At: "12:00", Earliest: "20:00", Latest: "10:00", Action: "noop"}}},
		{ID: "p", Version: "1", Phases: []PhaseSpec{{ID: "a", At: "12:00", Offset: "25h", Action: "noop"}}},
		{ID: "p", Version: "1", Phases: []PhaseSpec{{ID: "a", At: "13:00", Action: "noop"}, {ID: "b", At: "12:00", Action: "noop"}}},
	}
	for _, spec := range cases {
		if _, err := CompilePolicy(spec, nil, "UTC"); err == nil {
			t.Fatalf("invalid policy accepted: %+v", spec)
		}
	}
}
func TestMissingSolarFallbackAndDSTSkip(t *testing.T) {
	tz := zone(t)
	calc := geo.NewCalculator(&geo.Location{Latitude: 80, Longitude: 25, Timezone: tz.String()})
	spec := PolicySpec{ID: "p", Version: "1", Phases: []PhaseSpec{{ID: "rise", At: "@sunrise", Fallback: "08:00", Action: "noop"}}}
	p, err := CompilePolicy(spec, calc, tz.String())
	if err != nil {
		t.Fatal(err)
	}
	preview, err := p.Preview("2026-12-21")
	if err != nil {
		t.Fatal(err)
	}
	if preview.Solar["sunrise"] != nil || preview.Phases[0].Time.Hour() != 8 {
		t.Fatal("missing event fallback not explicit")
	}
	spec.Phases[0].Fallback = ""
	p, err = CompilePolicy(spec, calc, tz.String())
	if err != nil {
		t.Fatal(err)
	}
	preview, err = p.Preview("2026-12-21")
	if err != nil || !preview.Phases[0].Skipped {
		t.Fatal("polar absent event invented")
	}
	spec.Phases[0].At = "03:30"
	p, err = CompilePolicy(spec, nil, tz.String())
	if err != nil {
		t.Fatal(err)
	}
	preview, err = p.Preview("2026-03-29")
	if err != nil || !preview.Phases[0].Skipped {
		t.Fatal("DST gap not skipped")
	}
}
func TestPureLoaderRejectsEffectsAndDynamicCode(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "should-not-exist")
	for _, source := range []string{
		`os.execute("touch ` + marker + `"); return {}`,
		`return {id=require("hue")}`,
		`while true do end; return {}`,
		`local x="a"; for i=1,50 do x=x..x end; return {id=x}`,
		`return {id="x".."y"}`,
		`return {id="p",version="1",unknown="field",phases={{id="a",at="12:00",action="noop"}}}`,
	} {
		path := filepath.Join(t.TempDir(), "policy.lua")
		if err := os.WriteFile(path, []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadPolicyFile(context.Background(), path); err == nil {
			t.Fatalf("unsafe/invalid policy accepted: %s", source)
		}
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("loader performed side effect")
	}
	ordinary := filepath.Join(t.TempDir(), "script.lua")
	if err := os.WriteFile(ordinary, []byte(`os.execute("touch `+marker+`"); error("not run")`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := CheckLuaSyntax(ordinary); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("syntax check executed script")
	}
}

func TestPolicyCopiesDefinitionAndSyntheticCIFixture(t *testing.T) {
	spec, err := LoadPolicyFile(context.Background(), "../../examples/seasonal-policy.lua")
	if err != nil {
		t.Fatal(err)
	}
	// Deliberately synthetic coordinate and timezone; no deployment configuration.
	p, err := CompilePolicy(spec, geo.NewCalculator(&geo.Location{Latitude: 0, Longitude: 0, Timezone: "UTC"}), "UTC")
	if err != nil {
		t.Fatal(err)
	}
	spec.Phases[0].Args["scene"] = "mutated"
	if got := p.Spec().Phases[0].Args["scene"]; got != "Energize" {
		t.Fatal("caller mutated installed policy")
	}
	returned := p.Spec()
	returned.Phases[0].Args["scene"] = "also mutated"
	if got := p.Spec().Phases[0].Args["scene"]; got != "Energize" {
		t.Fatal("Spec exposed mutable data")
	}
	if _, err := p.Preview("2026-12-21"); err != nil {
		t.Fatal(err)
	}
}
