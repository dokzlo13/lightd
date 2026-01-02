package storage

import (
	"database/sql"
	"time"

	"github.com/rs/zerolog/log"
)

// CachedLocation represents a geocoded location stored in the cache
type CachedLocation struct {
	Name      string
	Latitude  float64
	Longitude float64
}

// GeoCache provides persistent storage for geocoded locations
type GeoCache struct {
	db *sql.DB
}

// NewGeoCache creates a new geo cache backed by SQLite
func NewGeoCache(db *sql.DB) *GeoCache {
	return &GeoCache{db: db}
}

// Get retrieves a cached location by query string
func (c *GeoCache) Get(query string) (*CachedLocation, bool) {
	var loc CachedLocation
	err := c.db.QueryRow(`
		SELECT display_name, latitude, longitude
		FROM geocache
		WHERE query = ?
	`, query).Scan(&loc.Name, &loc.Latitude, &loc.Longitude)

	if err == sql.ErrNoRows {
		return nil, false
	}
	if err != nil {
		log.Warn().Err(err).Str("query", query).Msg("Failed to read geocache")
		return nil, false
	}

	log.Debug().Str("query", query).Float64("lat", loc.Latitude).Float64("lon", loc.Longitude).Msg("Geocache hit")
	return &loc, true
}

// Put stores a geocoded location
func (c *GeoCache) Put(query string, loc *CachedLocation) error {
	now := time.Now().Unix()
	_, err := c.db.Exec(`
		INSERT OR REPLACE INTO geocache (query, display_name, latitude, longitude, created_at)
		VALUES (?, ?, ?, ?, ?)
	`, query, loc.Name, loc.Latitude, loc.Longitude, now)

	if err != nil {
		log.Warn().Err(err).Str("query", query).Msg("Failed to write geocache")
		return err
	}

	log.Info().Str("query", query).Float64("lat", loc.Latitude).Float64("lon", loc.Longitude).Msg("Geocache stored")
	return nil
}
