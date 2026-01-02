package kv

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// Manager manages bucket lifecycle and provides access to buckets.
type Manager struct {
	db             *sql.DB
	buckets        map[string]Bucket
	mu             sync.RWMutex
	cleanupStop    chan struct{}
	cleanupStopped chan struct{}
}

// NewManager creates a new KV manager.
func NewManager(db *sql.DB) *Manager {
	return &Manager{
		db:      db,
		buckets: make(map[string]Bucket),
	}
}

// Bucket returns a bucket by name, creating it if it doesn't exist.
// If persistent is true, the bucket is backed by SQLite; otherwise it's in-memory.
func (m *Manager) Bucket(name string, persistent bool) Bucket {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if bucket already exists
	if bucket, ok := m.buckets[name]; ok {
		return bucket
	}

	// Create new bucket
	var bucket Bucket
	if persistent {
		bucket = NewSQLiteBucket(m.db, name)
	} else {
		bucket = NewMemoryBucket(name)
	}

	m.buckets[name] = bucket
	log.Debug().
		Str("bucket", name).
		Bool("persistent", persistent).
		Msg("Created KV bucket")

	return bucket
}

// Exists returns true if a bucket with the given name exists.
func (m *Manager) Exists(name string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if _, ok := m.buckets[name]; ok {
		return true
	}

	// For persistent buckets, check the database
	var count int
	err := m.db.QueryRow(`
		SELECT COUNT(DISTINCT bucket) FROM kv_store WHERE bucket = ?
	`, name).Scan(&count)
	if err != nil {
		return false
	}

	return count > 0
}

// Delete removes a bucket and all its data.
func (m *Manager) Delete(name string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Remove from memory registry
	delete(m.buckets, name)

	// Remove from database
	result, err := m.db.Exec(`DELETE FROM kv_store WHERE bucket = ?`, name)
	if err != nil {
		return false, fmt.Errorf("failed to delete bucket: %w", err)
	}

	affected, _ := result.RowsAffected()
	if affected > 0 {
		log.Debug().Str("bucket", name).Int64("keys_deleted", affected).Msg("Deleted KV bucket")
	}

	return affected > 0, nil
}

// List returns all known bucket names.
func (m *Manager) List() ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Start with in-memory buckets
	seen := make(map[string]bool)
	for name := range m.buckets {
		seen[name] = true
	}

	// Add persistent buckets from database
	rows, err := m.db.Query(`SELECT DISTINCT bucket FROM kv_store`)
	if err != nil {
		return nil, fmt.Errorf("failed to list buckets: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("failed to scan bucket name: %w", err)
		}
		seen[name] = true
	}

	// Convert to slice
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}

	return names, rows.Err()
}

// StartCleanup starts a background goroutine that periodically cleans up expired entries.
func (m *Manager) StartCleanup(ctx context.Context, interval time.Duration) {
	m.cleanupStop = make(chan struct{})
	m.cleanupStopped = make(chan struct{})

	go func() {
		defer close(m.cleanupStopped)

		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-m.cleanupStop:
				return
			case <-ticker.C:
				m.cleanup()
			}
		}
	}()

	log.Debug().Dur("interval", interval).Msg("Started KV cleanup goroutine")
}

// StopCleanup stops the background cleanup goroutine.
func (m *Manager) StopCleanup() {
	if m.cleanupStop != nil {
		close(m.cleanupStop)
		<-m.cleanupStopped
		log.Debug().Msg("Stopped KV cleanup goroutine")
	}
}

// cleanup removes expired entries from all buckets.
func (m *Manager) cleanup() {
	// Cleanup SQLite entries
	count, err := CleanupExpired(m.db)
	if err != nil {
		log.Warn().Err(err).Msg("Failed to cleanup expired KV entries from SQLite")
	} else if count > 0 {
		log.Debug().Int64("count", count).Msg("Cleaned up expired KV entries from SQLite")
	}

	// Cleanup memory buckets
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, bucket := range m.buckets {
		if mb, ok := bucket.(*MemoryBucket); ok {
			if cleaned := mb.CleanupExpired(); cleaned > 0 {
				log.Debug().
					Str("bucket", mb.Name()).
					Int("count", cleaned).
					Msg("Cleaned up expired KV entries from memory bucket")
			}
		}
	}
}

