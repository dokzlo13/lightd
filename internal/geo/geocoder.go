package geo

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/dokzlo13/lightd/internal/storage"
)

// Default HTTP client (timeout set per-request via context)
var httpClient = &http.Client{}

const (
	// DefaultHTTPTimeout is the default timeout for geocoding requests
	DefaultHTTPTimeout = 10 * time.Second
)

// Location represents a geographic location with coordinates
type Location struct {
	Name      string  `json:"name"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Timezone  string  `json:"timezone"`
}

// Geocoder resolves location names to coordinates.
// It supports multiple resolution strategies:
//  1. Pre-configured coordinates (fastest, no network)
//  2. Persistent cache (SQLite, survives restarts)
//  3. Nominatim API (network call, cached for future use)
type Geocoder struct {
	cache       *storage.GeoCache
	httpTimeout time.Duration
}

// NewGeocoder creates a geocoder with optional persistent cache.
// If cache is nil, geocoding results won't be persisted.
func NewGeocoder(httpTimeout time.Duration, cache *storage.GeoCache) *Geocoder {
	if httpTimeout == 0 {
		httpTimeout = DefaultHTTPTimeout
	}
	return &Geocoder{
		cache:       cache,
		httpTimeout: httpTimeout,
	}
}

// Resolve resolves a location name to coordinates.
// It first checks the persistent cache, then falls back to Nominatim.
// Results are cached for future use.
func (g *Geocoder) Resolve(ctx context.Context, name string) (*Location, error) {
	if name == "" {
		return nil, fmt.Errorf("location name is empty")
	}

	// Check persistent cache first
	if g.cache != nil {
		if cached, found := g.cache.Get(name); found {
			log.Debug().
				Str("query", name).
				Str("name", cached.Name).
				Float64("lat", cached.Latitude).
				Float64("lon", cached.Longitude).
				Msg("Location resolved from cache")

			return &Location{
				Name:      cached.Name,
				Latitude:  cached.Latitude,
				Longitude: cached.Longitude,
			}, nil
		}
	}

	// Geocode via Nominatim
	loc, err := g.geocodeNominatim(ctx, name)
	if err != nil {
		return nil, err
	}

	// Store in persistent cache for future runs
	if g.cache != nil {
		if err := g.cache.Put(name, &storage.CachedLocation{
			Name:      loc.Name,
			Latitude:  loc.Latitude,
			Longitude: loc.Longitude,
		}); err != nil {
			return nil, fmt.Errorf("persist geocoding cache: %w", err)
		}
		log.Debug().Str("query", name).Msg("Location cached for future use")
	}

	return loc, nil
}

// geocodeNominatim performs geocoding via OpenStreetMap's Nominatim API
func (g *Geocoder) geocodeNominatim(ctx context.Context, name string) (*Location, error) {
	// Create request with timeout
	reqCtx, cancel := context.WithTimeout(ctx, g.httpTimeout)
	defer cancel()

	apiURL := fmt.Sprintf("https://nominatim.openstreetmap.org/search?q=%s&format=json&limit=1",
		url.QueryEscape(name))

	req, err := http.NewRequestWithContext(reqCtx, "GET", apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create geocoding request: %w", err)
	}
	req.Header.Set("User-Agent", "lightd/1.0")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("geocoding request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("geocoding failed with status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20+1))
	if err != nil {
		return nil, fmt.Errorf("failed to read geocoding response: %w", err)
	}

	if len(body) > 1<<20 {
		return nil, fmt.Errorf("geocoding response exceeds 1 MiB")
	}

	var results []struct {
		Lat         string `json:"lat"`
		Lon         string `json:"lon"`
		DisplayName string `json:"display_name"`
	}
	if err := json.Unmarshal(body, &results); err != nil {
		return nil, fmt.Errorf("failed to parse geocoding response: %w", err)
	}

	if len(results) == 0 {
		return nil, fmt.Errorf("configured location not found")
	}

	lat, latErr := strconv.ParseFloat(results[0].Lat, 64)
	lon, lonErr := strconv.ParseFloat(results[0].Lon, 64)
	if latErr != nil || lonErr != nil || math.IsNaN(lat) || math.IsNaN(lon) || math.IsInf(lat, 0) || math.IsInf(lon, 0) || lat < -90 || lat > 90 || lon < -180 || lon > 180 {
		return nil, fmt.Errorf("geocoder returned invalid coordinates")
	}

	loc := &Location{
		Name:      results[0].DisplayName,
		Latitude:  lat,
		Longitude: lon,
	}

	log.Debug().
		Str("query", name).
		Str("resolved", loc.Name).
		Float64("lat", lat).
		Float64("lon", lon).
		Msg("Location geocoded via Nominatim")

	return loc, nil
}

// ResolveOnBoot resolves location at application startup.
// This is a convenience method that handles all resolution strategies:
//  1. If lat/lon are provided, creates Location directly (no network)
//  2. If only location name is provided, resolves via Nominatim (with caching)
//  3. Returns error if neither is provided
//
// The timezone parameter is optional; if empty, it should be set separately.
func ResolveOnBoot(ctx context.Context, geocoder *Geocoder, locationName string, lat, lon float64, timezone string) (*Location, error) {
	// If coordinates are provided, use them directly
	if lat != 0 || lon != 0 {
		loc := &Location{
			Name:      locationName,
			Latitude:  lat,
			Longitude: lon,
			Timezone:  timezone,
		}
		log.Debug().
			Str("name", locationName).
			Float64("lat", lat).
			Float64("lon", lon).
			Str("timezone", timezone).
			Msg("Using pre-configured coordinates")
		return loc, nil
	}

	// If only location name is provided, geocode it
	if locationName != "" {
		loc, err := geocoder.Resolve(ctx, locationName)
		if err != nil {
			return nil, fmt.Errorf("failed to geocode configured location: %w", err)
		}
		// Set timezone if provided (geocoding doesn't return timezone)
		if timezone != "" {
			loc.Timezone = timezone
		}
		return loc, nil
	}

	return nil, fmt.Errorf("no location configured: set either lat/lon or location name")
}
