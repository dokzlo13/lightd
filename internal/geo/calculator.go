package geo

import (
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// maxAstroCacheEntries limits the in-memory cache size for astronomical times.
// Each entry is keyed by date and contains sunrise/sunset times for one day.
// Without this limit, the cache would grow by 1 entry per day indefinitely.
// We keep 14 days worth of entries which is sufficient for:
//   - Today's schedule calculations
//   - Next-day lookups (for schedules that span midnight)
//   - Some buffer for timezone edge cases
//
// Recalculation is cheap (pure math, ~1µs) so eviction has negligible performance impact.
const maxAstroCacheEntries = 14

// AstroTimes contains astronomical times for a day
type AstroTimes struct {
	Dawn     time.Time `json:"dawn"`
	Sunrise  time.Time `json:"sunrise"`
	Noon     time.Time `json:"noon"`
	Sunset   time.Time `json:"sunset"`
	Dusk     time.Time `json:"dusk"`
	Midnight time.Time `json:"midnight"`
}

// Calculator calculates astronomical times for a fixed location.
// It performs pure mathematical calculations with no network I/O.
// All geocoding should be done before creating the Calculator.
type Calculator struct {
	location *Location
	tz       *time.Location

	mu    sync.RWMutex
	cache map[string]*AstroTimes // cache by date string "2006-01-02"
}

// NewCalculator creates a new astronomical calculator for the given location.
// The location must have valid Latitude and Longitude.
// If location is nil, a dummy calculator is created that returns zero times.
func NewCalculator(loc *Location) *Calculator {
	c := &Calculator{
		location: loc,
		cache:    make(map[string]*AstroTimes),
	}

	if loc == nil {
		log.Warn().Msg("Geo calculator created without location (astronomical times disabled)")
		return c
	}

	// Load timezone
	if loc.Timezone != "" {
		tz, err := time.LoadLocation(loc.Timezone)
		if err != nil {
			log.Warn().
				Err(err).
				Str("timezone", loc.Timezone).
				Msg("Failed to load timezone, using UTC")
			c.tz = time.UTC
		} else {
			c.tz = tz
		}
	} else {
		c.tz = time.UTC
	}

	log.Info().
		Str("name", loc.Name).
		Float64("lat", loc.Latitude).
		Float64("lon", loc.Longitude).
		Str("timezone", c.tz.String()).
		Msg("Geo calculator initialized")

	return c
}

// Location returns the calculator's location (may be nil if not configured)
func (c *Calculator) Location() *Location {
	return c.location
}

// IsConfigured returns true if the calculator has a valid location
func (c *Calculator) IsConfigured() bool {
	return c.location != nil
}

// GetTimes returns astronomical times for the given date.
// If no location is configured, returns nil with an error.
func (c *Calculator) GetTimes(date time.Time) (*AstroTimes, error) {
	if c.location == nil {
		return nil, fmt.Errorf("no location configured")
	}

	// Normalize to start of day in the calculator's timezone
	dateInTz := date.In(c.tz)
	dateKey := dateInTz.Format("2006-01-02")

	// Check cache
	c.mu.RLock()
	cached, ok := c.cache[dateKey]
	c.mu.RUnlock()
	if ok {
		return cached, nil
	}

	// Calculate times
	times := c.calculate(dateInTz)

	// Cache result with eviction when over limit
	c.mu.Lock()
	c.cache[dateKey] = times
	if len(c.cache) > maxAstroCacheEntries {
		// Simple eviction: clear all and keep only current entry.
		// This is fine because recalculation is cheap (~1µs pure math).
		c.cache = make(map[string]*AstroTimes)
		c.cache[dateKey] = times
	}
	c.mu.Unlock()

	return times, nil
}

// GetTimesForToday returns astronomical times for today in the calculator's timezone.
func (c *Calculator) GetTimesForToday() (*AstroTimes, error) {
	return c.GetTimes(time.Now())
}

// calculate computes astronomical times using solar calculations
func (c *Calculator) calculate(date time.Time) *AstroTimes {
	lat := c.location.Latitude
	lon := c.location.Longitude

	// Julian day - add 0.5 because the NOAA sunrise equation expects JD at noon, not midnight
	jd := toJulianDay(date) + 0.5

	// Solar noon
	noon := solarNoon(jd, lon, c.tz, date)

	// Sun times
	sunrise := sunTime(jd, lat, lon, c.tz, date, -0.833, true)
	sunset := sunTime(jd, lat, lon, c.tz, date, -0.833, false)
	dawn := sunTime(jd, lat, lon, c.tz, date, -6.0, true)  // Civil dawn
	dusk := sunTime(jd, lat, lon, c.tz, date, -6.0, false) // Civil dusk

	// Midnight is next day at 00:00
	midnight := time.Date(date.Year(), date.Month(), date.Day()+1, 0, 0, 0, 0, c.tz)

	return &AstroTimes{
		Dawn:     dawn,
		Sunrise:  sunrise,
		Noon:     noon,
		Sunset:   sunset,
		Dusk:     dusk,
		Midnight: midnight,
	}
}

// toJulianDay converts a date to Julian day number
func toJulianDay(t time.Time) float64 {
	y := float64(t.Year())
	m := float64(t.Month())
	d := float64(t.Day())

	if m <= 2 {
		y--
		m += 12
	}

	a := math.Floor(y / 100)
	b := 2 - a + math.Floor(a/4)

	return math.Floor(365.25*(y+4716)) + math.Floor(30.6001*(m+1)) + d + b - 1524.5
}

// solarNoon calculates solar noon
func solarNoon(jd, lon float64, tz *time.Location, date time.Time) time.Time {
	// Approximate solar noon
	n := jd - 2451545.0 + 0.0008

	// Mean solar noon
	jStar := n - lon/360.0

	// Solar mean anomaly
	m := math.Mod(357.5291+0.98560028*jStar, 360.0)
	mRad := m * math.Pi / 180.0

	// Equation of center
	c := 1.9148*math.Sin(mRad) + 0.02*math.Sin(2*mRad) + 0.0003*math.Sin(3*mRad)

	// Ecliptic longitude
	lambda := math.Mod(m+c+180+102.9372, 360.0)
	lambdaRad := lambda * math.Pi / 180.0

	// Solar transit
	jTransit := 2451545.0 + jStar + 0.0053*math.Sin(mRad) - 0.0069*math.Sin(2*lambdaRad)

	// Convert to time
	return julianToTime(jTransit, tz, date)
}

// sunTime calculates sunrise or sunset time
func sunTime(jd, lat, lon float64, tz *time.Location, date time.Time, angle float64, rising bool) time.Time {
	// Approximate solar noon
	n := jd - 2451545.0 + 0.0008
	jStar := n - lon/360.0

	// Solar mean anomaly
	m := math.Mod(357.5291+0.98560028*jStar, 360.0)
	mRad := m * math.Pi / 180.0

	// Equation of center
	c := 1.9148*math.Sin(mRad) + 0.02*math.Sin(2*mRad) + 0.0003*math.Sin(3*mRad)

	// Ecliptic longitude
	lambda := math.Mod(m+c+180+102.9372, 360.0)
	lambdaRad := lambda * math.Pi / 180.0

	// Solar transit
	jTransit := 2451545.0 + jStar + 0.0053*math.Sin(mRad) - 0.0069*math.Sin(2*lambdaRad)

	// Declination of the sun
	sinDec := math.Sin(lambdaRad) * math.Sin(23.44*math.Pi/180.0)
	dec := math.Asin(sinDec)

	// Hour angle
	latRad := lat * math.Pi / 180.0
	angleRad := angle * math.Pi / 180.0

	cosOmega := (math.Sin(angleRad) - math.Sin(latRad)*math.Sin(dec)) / (math.Cos(latRad) * math.Cos(dec))

	// Clamp to valid range
	if cosOmega > 1 {
		cosOmega = 1
	} else if cosOmega < -1 {
		cosOmega = -1
	}

	omega := math.Acos(cosOmega) * 180.0 / math.Pi

	var jTime float64
	if rising {
		jTime = jTransit - omega/360.0
	} else {
		jTime = jTransit + omega/360.0
	}

	return julianToTime(jTime, tz, date)
}

// julianToTime converts Julian day to time.Time
func julianToTime(jd float64, tz *time.Location, refDate time.Time) time.Time {
	// Convert Julian day to Unix timestamp
	unixTime := (jd - 2440587.5) * 86400.0
	t := time.Unix(int64(unixTime), int64((unixTime-math.Floor(unixTime))*1e9))

	// Adjust to the reference date's timezone
	return time.Date(
		refDate.Year(), refDate.Month(), refDate.Day(),
		t.Hour(), t.Minute(), t.Second(), 0, tz,
	)
}
