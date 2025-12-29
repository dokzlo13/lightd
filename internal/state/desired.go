// Package state provides the desired state store for HuePlanner.
// Desired state is versioned for dirty tracking in the reconciler.
package state

import (
	"database/sql"
	"sync"
	"time"
)

// DesiredState represents the desired state for a single group
type DesiredState struct {
	GroupID       string
	BankSceneName sql.NullString // Nullable: scene to apply when on
	DesiredPower  sql.NullInt64  // NULL=unset, 0=off, 1=on
	Version       int64          // Incremented on every change
	UpdatedAt     time.Time
}

// Power returns the desired power state as a *bool (nil if unset)
func (d *DesiredState) Power() *bool {
	if !d.DesiredPower.Valid {
		return nil
	}
	v := d.DesiredPower.Int64 == 1
	return &v
}

// Bank returns the desired bank scene name (empty if unset)
func (d *DesiredState) Bank() string {
	if !d.BankSceneName.Valid {
		return ""
	}
	return d.BankSceneName.String
}

// HasBank returns true if a bank scene is set
func (d *DesiredState) HasBank() bool {
	return d.BankSceneName.Valid && d.BankSceneName.String != ""
}

// DesiredStore provides versioned desired state storage
type DesiredStore struct {
	db *sql.DB
	mu sync.RWMutex
}

// NewDesiredStore creates a new DesiredStore
func NewDesiredStore(db *sql.DB) *DesiredStore {
	return &DesiredStore{db: db}
}

// Get retrieves the desired state for a group
func (s *DesiredStore) Get(groupID string) (*DesiredState, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var state DesiredState
	var updatedAt int64

	err := s.db.QueryRow(`
		SELECT group_id, bank_scene_name, desired_power, version, updated_at
		FROM desired_state
		WHERE group_id = ?
	`, groupID).Scan(
		&state.GroupID, &state.BankSceneName, &state.DesiredPower, &state.Version, &updatedAt,
	)

	if err == sql.ErrNoRows {
		// Return empty state for non-existent group
		return &DesiredState{GroupID: groupID}, nil
	}
	if err != nil {
		return nil, err
	}

	state.UpdatedAt = time.Unix(updatedAt, 0).UTC()
	return &state, nil
}

// GetWithVersion retrieves the desired state and version atomically
func (s *DesiredStore) GetWithVersion(groupID string) (*DesiredState, int64) {
	state, err := s.Get(groupID)
	if err != nil || state == nil {
		return &DesiredState{GroupID: groupID}, 0
	}
	return state, state.Version
}

// SetBank sets the bank scene name for a group
func (s *DesiredStore) SetBank(groupID string, sceneName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC().Unix()

	_, err := s.db.Exec(`
		INSERT INTO desired_state (group_id, bank_scene_name, version, updated_at)
		VALUES (?, ?, 1, ?)
		ON CONFLICT(group_id) DO UPDATE SET
			bank_scene_name = excluded.bank_scene_name,
			version = version + 1,
			updated_at = excluded.updated_at
	`, groupID, sceneName, now)

	return err
}

// SetPower sets the desired power state for a group
func (s *DesiredStore) SetPower(groupID string, on bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC().Unix()
	var power int64
	if on {
		power = 1
	}

	_, err := s.db.Exec(`
		INSERT INTO desired_state (group_id, desired_power, version, updated_at)
		VALUES (?, ?, 1, ?)
		ON CONFLICT(group_id) DO UPDATE SET
			desired_power = excluded.desired_power,
			version = version + 1,
			updated_at = excluded.updated_at
	`, groupID, power, now)

	return err
}

// ClearPower clears the desired power state for a group (sets to NULL)
func (s *DesiredStore) ClearPower(groupID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC().Unix()

	_, err := s.db.Exec(`
		INSERT INTO desired_state (group_id, desired_power, version, updated_at)
		VALUES (?, NULL, 1, ?)
		ON CONFLICT(group_id) DO UPDATE SET
			desired_power = NULL,
			version = version + 1,
			updated_at = excluded.updated_at
	`, groupID, now)

	return err
}

// HasBank checks if a group has a bank scene set
func (s *DesiredStore) HasBank(groupID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var bankSceneName sql.NullString
	err := s.db.QueryRow(`
		SELECT bank_scene_name FROM desired_state WHERE group_id = ?
	`, groupID).Scan(&bankSceneName)

	return err == nil && bankSceneName.Valid && bankSceneName.String != ""
}

// GetBank returns the bank scene name for a group, or empty string if not set
func (s *DesiredStore) GetBank(groupID string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var bankSceneName sql.NullString
	err := s.db.QueryRow(`
		SELECT bank_scene_name FROM desired_state WHERE group_id = ?
	`, groupID).Scan(&bankSceneName)

	if err != nil || !bankSceneName.Valid {
		return ""
	}
	return bankSceneName.String
}

// GetAll returns all desired state entries
func (s *DesiredStore) GetAll() ([]*DesiredState, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query(`
		SELECT group_id, bank_scene_name, desired_power, version, updated_at
		FROM desired_state
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var states []*DesiredState
	for rows.Next() {
		var state DesiredState
		var updatedAt int64

		err := rows.Scan(
			&state.GroupID, &state.BankSceneName, &state.DesiredPower, &state.Version, &updatedAt,
		)
		if err != nil {
			return nil, err
		}

		state.UpdatedAt = time.Unix(updatedAt, 0).UTC()
		states = append(states, &state)
	}

	return states, rows.Err()
}

// GetDirtyGroups returns groups with version > the specified map of last reconciled versions
func (s *DesiredStore) GetDirtyGroups(lastVersions map[string]int64) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query(`
		SELECT group_id, version FROM desired_state
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var dirty []string
	for rows.Next() {
		var groupID string
		var version int64

		if err := rows.Scan(&groupID, &version); err != nil {
			return nil, err
		}

		lastVersion := lastVersions[groupID]
		if version > lastVersion {
			dirty = append(dirty, groupID)
		}
	}

	return dirty, rows.Err()
}

// Clear removes all desired state (for testing)
func (s *DesiredStore) Clear() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(`DELETE FROM desired_state`)
	return err
}

