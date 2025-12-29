package scheduler

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// MisfirePolicy defines how to handle missed schedule occurrences
type MisfirePolicy string

const (
	MisfirePolicySkip      MisfirePolicy = "skip"       // Skip all missed occurrences
	MisfirePolicyRunLatest MisfirePolicy = "run_latest" // Run only the latest missed
	MisfirePolicyRunAll    MisfirePolicy = "run_all"    // Run all missed in order
)

// Definition represents a stable schedule rule
type Definition struct {
	ID            string
	TimeExpr      string
	ActionName    string
	ActionArgs    map[string]any
	Tag           string
	MisfirePolicy MisfirePolicy
	Enabled       bool
	CreatedAt     time.Time

	// Parsed time expression (not stored in DB)
	parsedExpr *TimeExpr
}

// ParsedTimeExpr returns the parsed time expression
func (d *Definition) ParsedTimeExpr() (*TimeExpr, error) {
	if d.parsedExpr != nil {
		return d.parsedExpr, nil
	}

	expr, err := ParseTimeExpr(d.TimeExpr)
	if err != nil {
		return nil, err
	}
	d.parsedExpr = expr
	return expr, nil
}

// Occurrence represents a computed schedule occurrence
type Occurrence struct {
	DefID        string
	OccurrenceID string // "def_id/run_at_unix"
	RunAt        time.Time
	IsNext       bool
}

// OccurrenceID generates an occurrence ID from definition ID and run time
func OccurrenceID(defID string, runAt time.Time) string {
	return fmt.Sprintf("%s/%d", defID, runAt.UTC().Unix())
}

// DefinitionStore provides persistence for schedule definitions
type DefinitionStore struct {
	db *sql.DB
}

// NewDefinitionStore creates a new definition store
func NewDefinitionStore(db *sql.DB) *DefinitionStore {
	return &DefinitionStore{db: db}
}

// Upsert inserts or updates a definition
func (s *DefinitionStore) Upsert(def *Definition) error {
	argsJSON, err := json.Marshal(def.ActionArgs)
	if err != nil {
		return fmt.Errorf("failed to marshal action args: %w", err)
	}

	var enabled int
	if def.Enabled {
		enabled = 1
	}

	_, err = s.db.Exec(`
		INSERT INTO schedule_definitions (id, time_expr, action_name, action_args, tag, misfire_policy, enabled, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			time_expr = excluded.time_expr,
			action_name = excluded.action_name,
			action_args = excluded.action_args,
			tag = excluded.tag,
			misfire_policy = excluded.misfire_policy,
			enabled = excluded.enabled
	`, def.ID, def.TimeExpr, def.ActionName, string(argsJSON), def.Tag,
		string(def.MisfirePolicy), enabled, def.CreatedAt.Unix())

	return err
}

// Get retrieves a definition by ID
func (s *DefinitionStore) Get(id string) (*Definition, error) {
	var def Definition
	var argsJSON string
	var enabled int
	var createdAt int64

	err := s.db.QueryRow(`
		SELECT id, time_expr, action_name, action_args, tag, misfire_policy, enabled, created_at
		FROM schedule_definitions
		WHERE id = ?
	`, id).Scan(
		&def.ID, &def.TimeExpr, &def.ActionName, &argsJSON, &def.Tag,
		&def.MisfirePolicy, &enabled, &createdAt,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	def.Enabled = enabled == 1
	def.CreatedAt = time.Unix(createdAt, 0).UTC()

	if argsJSON != "" {
		if err := json.Unmarshal([]byte(argsJSON), &def.ActionArgs); err != nil {
			return nil, fmt.Errorf("failed to unmarshal action args: %w", err)
		}
	}

	return &def, nil
}

// GetByTag returns all definitions with the specified tag
func (s *DefinitionStore) GetByTag(tag string) ([]*Definition, error) {
	rows, err := s.db.Query(`
		SELECT id, time_expr, action_name, action_args, tag, misfire_policy, enabled, created_at
		FROM schedule_definitions
		WHERE tag = ? AND enabled = 1
		ORDER BY id
	`, tag)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return s.scanDefinitions(rows)
}

// GetAllEnabled returns all enabled definitions
func (s *DefinitionStore) GetAllEnabled() ([]*Definition, error) {
	rows, err := s.db.Query(`
		SELECT id, time_expr, action_name, action_args, tag, misfire_policy, enabled, created_at
		FROM schedule_definitions
		WHERE enabled = 1
		ORDER BY id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return s.scanDefinitions(rows)
}

// GetAll returns all definitions
func (s *DefinitionStore) GetAll() ([]*Definition, error) {
	rows, err := s.db.Query(`
		SELECT id, time_expr, action_name, action_args, tag, misfire_policy, enabled, created_at
		FROM schedule_definitions
		ORDER BY id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return s.scanDefinitions(rows)
}

func (s *DefinitionStore) scanDefinitions(rows *sql.Rows) ([]*Definition, error) {
	var defs []*Definition
	for rows.Next() {
		var def Definition
		var argsJSON string
		var enabled int
		var createdAt int64

		err := rows.Scan(
			&def.ID, &def.TimeExpr, &def.ActionName, &argsJSON, &def.Tag,
			&def.MisfirePolicy, &enabled, &createdAt,
		)
		if err != nil {
			return nil, err
		}

		def.Enabled = enabled == 1
		def.CreatedAt = time.Unix(createdAt, 0).UTC()

		if argsJSON != "" {
			def.ActionArgs = make(map[string]any)
			if err := json.Unmarshal([]byte(argsJSON), &def.ActionArgs); err != nil {
				return nil, fmt.Errorf("failed to unmarshal action args: %w", err)
			}
		}

		defs = append(defs, &def)
	}

	return defs, rows.Err()
}

// Delete removes a definition
func (s *DefinitionStore) Delete(id string) error {
	_, err := s.db.Exec(`DELETE FROM schedule_definitions WHERE id = ?`, id)
	return err
}

// SetEnabled enables or disables a definition
func (s *DefinitionStore) SetEnabled(id string, enabled bool) error {
	var e int
	if enabled {
		e = 1
	}
	_, err := s.db.Exec(`UPDATE schedule_definitions SET enabled = ? WHERE id = ?`, e, id)
	return err
}

// OccurrenceStore provides persistence for computed occurrences
type OccurrenceStore struct {
	db *sql.DB
}

// NewOccurrenceStore creates a new occurrence store
func NewOccurrenceStore(db *sql.DB) *OccurrenceStore {
	return &OccurrenceStore{db: db}
}

// Clear removes all occurrences for a definition
func (s *OccurrenceStore) Clear(defID string) error {
	_, err := s.db.Exec(`DELETE FROM schedule_occurrences WHERE def_id = ?`, defID)
	return err
}

// ClearAll removes all occurrences
func (s *OccurrenceStore) ClearAll() error {
	_, err := s.db.Exec(`DELETE FROM schedule_occurrences`)
	return err
}

// Insert adds an occurrence
func (s *OccurrenceStore) Insert(occ *Occurrence) error {
	var isNext int
	if occ.IsNext {
		isNext = 1
	}

	_, err := s.db.Exec(`
		INSERT OR REPLACE INTO schedule_occurrences (def_id, occurrence_id, run_at, is_next)
		VALUES (?, ?, ?, ?)
	`, occ.DefID, occ.OccurrenceID, occ.RunAt.Unix(), isNext)

	return err
}

// GetNext returns the next occurrence to run (earliest with is_next=1)
func (s *OccurrenceStore) GetNext() (*Occurrence, error) {
	var occ Occurrence
	var runAt int64
	var isNext int

	err := s.db.QueryRow(`
		SELECT def_id, occurrence_id, run_at, is_next
		FROM schedule_occurrences
		WHERE is_next = 1
		ORDER BY run_at ASC
		LIMIT 1
	`).Scan(&occ.DefID, &occ.OccurrenceID, &runAt, &isNext)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	occ.RunAt = time.Unix(runAt, 0).UTC()
	occ.IsNext = isNext == 1
	return &occ, nil
}

// GetPending returns all occurrences that should run before the given time
func (s *OccurrenceStore) GetPending(before time.Time) ([]*Occurrence, error) {
	rows, err := s.db.Query(`
		SELECT def_id, occurrence_id, run_at, is_next
		FROM schedule_occurrences
		WHERE run_at <= ? AND is_next = 1
		ORDER BY run_at ASC
	`, before.Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return s.scanOccurrences(rows)
}

// GetByDefID returns all occurrences for a definition
func (s *OccurrenceStore) GetByDefID(defID string) ([]*Occurrence, error) {
	rows, err := s.db.Query(`
		SELECT def_id, occurrence_id, run_at, is_next
		FROM schedule_occurrences
		WHERE def_id = ?
		ORDER BY run_at ASC
	`, defID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return s.scanOccurrences(rows)
}

func (s *OccurrenceStore) scanOccurrences(rows *sql.Rows) ([]*Occurrence, error) {
	var occs []*Occurrence
	for rows.Next() {
		var occ Occurrence
		var runAt int64
		var isNext int

		err := rows.Scan(&occ.DefID, &occ.OccurrenceID, &runAt, &isNext)
		if err != nil {
			return nil, err
		}

		occ.RunAt = time.Unix(runAt, 0).UTC()
		occ.IsNext = isNext == 1
		occs = append(occs, &occ)
	}

	return occs, rows.Err()
}

// MarkProcessed marks an occurrence as processed (removes is_next flag)
func (s *OccurrenceStore) MarkProcessed(occurrenceID string) error {
	_, err := s.db.Exec(`
		UPDATE schedule_occurrences SET is_next = 0 WHERE occurrence_id = ?
	`, occurrenceID)
	return err
}

