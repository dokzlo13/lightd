package geo

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func helsinki(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Europe/Helsinki")
	if err != nil {
		t.Fatal(err)
	}
	return loc
}
func TestSolarInstantsIndependentOfProcessZoneAndPreserveDate(t *testing.T) {
	tz := helsinki(t)
	old := time.Local
	defer func() { time.Local = old }()
	loc := &Location{Latitude: 60.17, Longitude: 24.94, Timezone: tz.String()}
	date := time.Date(2026, 6, 21, 12, 0, 0, 0, tz)
	time.Local = time.UTC
	a, err := NewCalculator(loc).GetTimes(date)
	if err != nil {
		t.Fatal(err)
	}
	time.Local = tz
	b, err := NewCalculator(loc).GetTimes(date)
	if err != nil {
		t.Fatal(err)
	}
	if !a.Dawn.Equal(b.Dawn) || !a.Sunset.Equal(b.Sunset) || !a.Dusk.Equal(b.Dusk) {
		t.Fatal("solar values depend on process timezone")
	}
	if !a.Dusk.After(a.Sunset) || a.Dusk.Day() != 22 {
		t.Fatalf("dusk rollover lost: sunset=%v dusk=%v", a.Sunset, a.Dusk)
	}
}
func TestSolarIndependentUSNOReference(t *testing.T) {
	// Public Helsinki coordinate fixture, USNO API v4.0.1 retrieved 2026-10-25:
	// https://aa.usno.navy.mil/api/rstt/oneday?date=2026-12-21&coords=60.17,24.94&tz=2
	// USNO rounds to minutes. The approximation and atmospheric convention differ;
	// a three-minute tolerance covers those differences, not timezone/date errors.
	tz := helsinki(t)
	calc := NewCalculator(&Location{Latitude: 60.17, Longitude: 24.94, Timezone: tz.String()})
	date := time.Date(2026, 12, 21, 12, 0, 0, 0, tz)
	got, err := calc.GetTimes(date)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, want string
		actual     time.Time
	}{{"dawn", "08:25", got.Dawn}, {"sunrise", "09:24", got.Sunrise}, {"noon", "12:18", got.Noon}, {"sunset", "15:13", got.Sunset}, {"dusk", "16:11", got.Dusk}} {
		want, err := time.ParseInLocation("2006-01-02 15:04", "2026-12-21 "+test.want, tz)
		if err != nil {
			t.Fatal(err)
		}
		delta := test.actual.Sub(want)
		if delta < -3*time.Minute || delta > 3*time.Minute {
			t.Errorf("%s got=%s USNO=%s delta=%s", test.name, test.actual, want, delta)
		}
	}
}
func TestPolarCrossingsStayAbsent(t *testing.T) {
	tz := helsinki(t)
	calc := NewCalculator(&Location{Latitude: 80, Longitude: 25, Timezone: tz.String()})
	for _, date := range []time.Time{time.Date(2026, 12, 21, 12, 0, 0, 0, tz), time.Date(2026, 6, 21, 12, 0, 0, 0, tz)} {
		got, err := calc.GetTimes(date)
		if err != nil {
			t.Fatal(err)
		}
		if !got.Sunrise.IsZero() || !got.Sunset.IsZero() {
			t.Fatalf("invented horizon crossing at %v", date)
		}
		if got.Noon.IsZero() {
			t.Fatal("solar noon absent")
		}
	}
}

type fixtureTransport func(*http.Request) (*http.Response, error)

func (f fixtureTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestGeocoderRejectsInvalidAndOversizedPayload(t *testing.T) {
	old := httpClient
	defer func() { httpClient = old }()
	for _, body := range []string{`[{"lat":"oops","lon":"oops"}]`, `[{"lat":"91","lon":"181"}]`, `[{"lat":"NaN","lon":"25"}]`, strings.Repeat("x", (1<<20)+1)} {
		httpClient = &http.Client{Transport: fixtureTransport(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
		})}
		if _, err := NewGeocoder(time.Second, nil).geocodeNominatim(context.Background(), "fixture"); err == nil {
			t.Fatal("invalid geocoding accepted")
		}
	}
}
