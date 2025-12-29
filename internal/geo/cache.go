package geo

import (
	"database/sql"
	"time"

	"github.com/rs/zerolog/log"
)

// Cache provides persistent storage for geocoded locations
type Cache struct {
	db *sql.DB
}

// NewCache creates a new geo cache backed by SQLite
func NewCache(db *sql.DB) *Cache {
	return &Cache{db: db}
}

// Get retrieves a cached location by query string
func (c *Cache) Get(query string) (*Location, bool) {
	var loc Location
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
func (c *Cache) Put(query string, loc *Location) error {
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

