package storage

import (
	"database/sql"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// Store provides generic versioned state storage with JSON payloads.
// State is keyed by (kind, id) and stored as JSON blobs with version tracking.
type Store struct {
	db *sql.DB
	mu sync.RWMutex
}

// NewStore creates a new generic state store.
func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

// Get retrieves payload and version for a resource.
// Returns empty payload and version 0 if not found.
func (s *Store) Get(kind, id string) (payload []byte, version int64, err error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var payloadStr string
	err = s.db.QueryRow(`
		SELECT payload, version FROM resource_state
		WHERE kind = ? AND id = ?
	`, kind, id).Scan(&payloadStr, &version)

	if err == sql.ErrNoRows {
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, err
	}

	return []byte(payloadStr), version, nil
}

// Set stores payload, incrementing version automatically.
// Creates new entry if not exists, updates if exists.
func (s *Store) Set(kind, id string, payload []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC().Unix()

	_, err := s.db.Exec(`
		INSERT INTO resource_state (kind, id, payload, version, updated_at)
		VALUES (?, ?, ?, 1, ?)
		ON CONFLICT(kind, id) DO UPDATE SET
			payload = excluded.payload,
			version = version + 1,
			updated_at = excluded.updated_at
	`, kind, id, string(payload), now)

	if err == nil {
		log.Debug().
			Str("kind", kind).
			Str("id", id).
			Str("payload", string(payload)).
			Msg("Store.Set completed")
	}

	return err
}

// GetDirty returns IDs where version > lastVersions[id] for a given kind.
// This is used by reconcilers to find resources that need reconciliation.
func (s *Store) GetDirty(kind string, lastVersions map[string]int64) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query(`
		SELECT id, version FROM resource_state WHERE kind = ?
	`, kind)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var dirty []string
	for rows.Next() {
		var id string
		var version int64

		if err := rows.Scan(&id, &version); err != nil {
			return nil, err
		}

		lastVersion := lastVersions[id]
		if version > lastVersion {
			dirty = append(dirty, id)
		}
	}

	return dirty, rows.Err()
}

// Delete removes a resource state entry.
func (s *Store) Delete(kind, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(`
		DELETE FROM resource_state WHERE kind = ? AND id = ?
	`, kind, id)

	return err
}

// Clear removes all state for a kind. If kind is empty, clears all state.
func (s *Store) Clear(kind string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var err error
	if kind == "" {
		_, err = s.db.Exec(`DELETE FROM resource_state`)
	} else {
		_, err = s.db.Exec(`DELETE FROM resource_state WHERE kind = ?`, kind)
	}

	return err
}

// GetAll returns all entries for a kind with their versions.
func (s *Store) GetAll(kind string) (map[string][]byte, map[string]int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query(`
		SELECT id, payload, version FROM resource_state WHERE kind = ?
	`, kind)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	payloads := make(map[string][]byte)
	versions := make(map[string]int64)

	for rows.Next() {
		var id, payloadStr string
		var version int64

		if err := rows.Scan(&id, &payloadStr, &version); err != nil {
			return nil, nil, err
		}

		payloads[id] = []byte(payloadStr)
		versions[id] = version
	}

	return payloads, versions, rows.Err()
}
