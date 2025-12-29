package geo

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// Default HTTP client (timeout set per-request via context)
var httpClient = &http.Client{}

// AstroTimes contains astronomical times for a day
type AstroTimes struct {
	Dawn     time.Time `json:"dawn"`
	Sunrise  time.Time `json:"sunrise"`
	Noon     time.Time `json:"noon"`
	Sunset   time.Time `json:"sunset"`
	Dusk     time.Time `json:"dusk"`
	Midnight time.Time `json:"midnight"`
}

// Calculator calculates astronomical times
type Calculator struct {
	mu    sync.RWMutex
	cache map[string]*AstroTimes // cache by "lat,lon,date"

	// Geocoded location cache (in-memory)
	locationCache map[string]*Location

	// Persistent geocache (optional, backed by SQLite)
	persistentCache *Cache

	// Pre-configured location (optional, avoids geocoding)
	defaultLocation *Location

	// HTTP timeout for geocoding requests
	httpTimeout time.Duration
}

// Location represents a geocoded location
type Location struct {
	Name      string
	Latitude  float64
	Longitude float64
	Timezone  string
}

// NewCalculator creates a new astronomical calculator without persistent cache
func NewCalculator() *Calculator {
	return &Calculator{
		cache:         make(map[string]*AstroTimes),
		locationCache: make(map[string]*Location),
	}
}

// NewCalculatorWithCache creates a calculator with persistent geocache
func NewCalculatorWithCache(httpTimeout time.Duration, persistentCache *Cache) *Calculator {
	if httpTimeout == 0 {
		httpTimeout = 10 * time.Second
	}
	return &Calculator{
		cache:           make(map[string]*AstroTimes),
		locationCache:   make(map[string]*Location),
		persistentCache: persistentCache,
		httpTimeout:     httpTimeout,
	}
}

// NewCalculatorWithLocation creates a calculator with pre-configured coordinates
// This avoids external geocoding calls entirely
func NewCalculatorWithLocation(name string, lat, lon float64, timezone string) *Calculator {
	loc := &Location{
		Name:      name,
		Latitude:  lat,
		Longitude: lon,
		Timezone:  timezone,
	}

	log.Info().
		Str("name", name).
		Float64("lat", lat).
		Float64("lon", lon).
		Msg("Geo calculator initialized with pre-configured coordinates")

	return &Calculator{
		cache:           make(map[string]*AstroTimes),
		locationCache:   make(map[string]*Location),
		defaultLocation: loc,
	}
}

// NewCalculatorWithLocationAndCache creates a calculator with both pre-configured coordinates and persistent cache
func NewCalculatorWithLocationAndCache(name string, lat, lon float64, timezone string, httpTimeout time.Duration, persistentCache *Cache) *Calculator {
	if httpTimeout == 0 {
		httpTimeout = 10 * time.Second
	}

	loc := &Location{
		Name:      name,
		Latitude:  lat,
		Longitude: lon,
		Timezone:  timezone,
	}

	log.Info().
		Str("name", name).
		Float64("lat", lat).
		Float64("lon", lon).
		Msg("Geo calculator initialized with pre-configured coordinates")

	return &Calculator{
		cache:           make(map[string]*AstroTimes),
		locationCache:   make(map[string]*Location),
		persistentCache: persistentCache,
		defaultLocation: loc,
		httpTimeout:     httpTimeout,
	}
}

// GetTimes returns astronomical times for a location on a given date
func (c *Calculator) GetTimes(locationName string, date time.Time, timezone string) (*AstroTimes, error) {
	// Get location coordinates (use pre-configured if available)
	loc, err := c.getLocation(locationName)
	if err != nil {
		return nil, fmt.Errorf("failed to get location: %w", err)
	}

	// Load timezone
	tz, err := time.LoadLocation(timezone)
	if err != nil {
		log.Warn().Err(err).Str("timezone", timezone).Msg("Failed to load timezone, using UTC")
		tz = time.UTC
	}

	// Check cache
	cacheKey := fmt.Sprintf("%.4f,%.4f,%s", loc.Latitude, loc.Longitude, date.Format("2006-01-02"))
	c.mu.RLock()
	cached, ok := c.cache[cacheKey]
	c.mu.RUnlock()
	if ok {
		return cached, nil
	}

	// Calculate times
	times := c.calculate(loc.Latitude, loc.Longitude, date, tz)

	// Cache result
	c.mu.Lock()
	c.cache[cacheKey] = times
	c.mu.Unlock()

	return times, nil
}

// getLocation returns coordinates for a location name
// Priority: pre-configured > persistent cache > in-memory cache > geocode
func (c *Calculator) getLocation(name string) (*Location, error) {
	// 1. If we have a pre-configured default location, use it
	if c.defaultLocation != nil {
		return c.defaultLocation, nil
	}

	// 2. Check in-memory cache
	c.mu.RLock()
	cached, ok := c.locationCache[name]
	c.mu.RUnlock()
	if ok {
		return cached, nil
	}

	// 3. Check persistent cache (SQLite)
	if c.persistentCache != nil {
		if loc, found := c.persistentCache.Get(name); found {
			// Also populate in-memory cache
			c.mu.Lock()
			c.locationCache[name] = loc
			c.mu.Unlock()
			return loc, nil
		}
	}

	// 4. Geocode using Nominatim (with timeout)
	loc, err := c.geocode(name)
	if err != nil {
		return nil, err
	}

	// Store in in-memory cache
	c.mu.Lock()
	c.locationCache[name] = loc
	c.mu.Unlock()

	// Store in persistent cache for future runs
	if c.persistentCache != nil {
		c.persistentCache.Put(name, loc)
	}

	return loc, nil
}

// geocode performs geocoding via Nominatim with proper timeout
func (c *Calculator) geocode(name string) (*Location, error) {
	timeout := c.httpTimeout
	if timeout == 0 {
		timeout = 10 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	apiURL := fmt.Sprintf("https://nominatim.openstreetmap.org/search?q=%s&format=json&limit=1",
		url.QueryEscape(name))

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "HuePlanner/2.0")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("geocoding request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("geocoding failed with status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var results []struct {
		Lat         string `json:"lat"`
		Lon         string `json:"lon"`
		DisplayName string `json:"display_name"`
	}
	if err := json.Unmarshal(body, &results); err != nil {
		return nil, err
	}

	if len(results) == 0 {
		return nil, fmt.Errorf("location not found: %s", name)
	}

	var lat, lon float64
	fmt.Sscanf(results[0].Lat, "%f", &lat)
	fmt.Sscanf(results[0].Lon, "%f", &lon)

	loc := &Location{
		Name:      results[0].DisplayName,
		Latitude:  lat,
		Longitude: lon,
	}

	log.Info().
		Str("query", name).
		Str("resolved", loc.Name).
		Float64("lat", lat).
		Float64("lon", lon).
		Msg("Location geocoded via Nominatim")

	return loc, nil
}

// calculate computes astronomical times using solar calculations
func (c *Calculator) calculate(lat, lon float64, date time.Time, tz *time.Location) *AstroTimes {
	// Julian day - add 0.5 because the NOAA sunrise equation expects JD at noon, not midnight
	jd := toJulianDay(date) + 0.5

	// Solar noon
	noon := solarNoon(jd, lon, tz, date)

	// Sun times
	sunrise := sunTime(jd, lat, lon, tz, date, -0.833, true)
	sunset := sunTime(jd, lat, lon, tz, date, -0.833, false)
	dawn := sunTime(jd, lat, lon, tz, date, -6.0, true)  // Civil dawn
	dusk := sunTime(jd, lat, lon, tz, date, -6.0, false) // Civil dusk

	// Midnight is next day at 00:00
	midnight := time.Date(date.Year(), date.Month(), date.Day()+1, 0, 0, 0, 0, tz)

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

// GetTimesForToday returns astronomical times for today
func (c *Calculator) GetTimesForToday(locationName string, timezone string) (*AstroTimes, error) {
	tz, err := time.LoadLocation(timezone)
	if err != nil {
		tz = time.UTC
	}
	today := time.Now().In(tz)
	return c.GetTimes(locationName, today, timezone)
}
