// Package ledger provides an append-only event history for HuePlanner.
// It supports action deduplication, misfire detection, and auditing.
package ledger

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// EventType represents the type of event in the ledger
type EventType string

const (
	EventActionStarted   EventType = "action_started"
	EventActionCompleted EventType = "action_completed"
	EventActionFailed    EventType = "action_failed"
	EventButtonProcessed EventType = "button_processed"
	EventScheduleFired   EventType = "schedule_fired"
)

// Entry represents a single event in the ledger
type Entry struct {
	ID             int64
	EventType      EventType
	Timestamp      time.Time
	Payload        map[string]any
	Source         string
	IdempotencyKey string
	DefID          string // For schedule-related events only
}

// Ledger provides append-only event logging with deduplication
type Ledger struct {
	db *sql.DB
}

// New creates a new Ledger using the provided database connection
func New(db *sql.DB) *Ledger {
	return &Ledger{db: db}
}

// Append adds a new event to the ledger
func (l *Ledger) Append(eventType EventType, idempotencyKey string, payload map[string]any) error {
	return l.AppendWithSource(eventType, idempotencyKey, "", "", payload)
}

// AppendWithSource adds a new event with source and def_id
// For action_completed events, uses INSERT OR IGNORE to ensure "first writer wins"
// and prevent duplicate completions (enforced by unique partial index)
func (l *Ledger) AppendWithSource(eventType EventType, idempotencyKey, source, defID string, payload map[string]any) error {
	var payloadJSON []byte
	var err error

	if payload != nil {
		payloadJSON, err = json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("failed to marshal payload: %w", err)
		}
	}

	now := time.Now().UTC().Unix()

	// Use INSERT OR IGNORE for action_completed to handle concurrent race conditions
	// The unique partial index on (idempotency_key) WHERE event_type = 'action_completed'
	// ensures only the first completion is recorded
	insertSQL := `INSERT INTO event_ledger (event_type, timestamp, payload, source, idempotency_key, def_id) VALUES (?, ?, ?, ?, ?, ?)`
	if eventType == EventActionCompleted && idempotencyKey != "" {
		insertSQL = `INSERT OR IGNORE INTO event_ledger (event_type, timestamp, payload, source, idempotency_key, def_id) VALUES (?, ?, ?, ?, ?, ?)`
	}

	_, err = l.db.Exec(insertSQL, string(eventType), now, string(payloadJSON), source, idempotencyKey, defID)

	return err
}

// HasCompleted checks if an action with the given idempotency_key has completed successfully
func (l *Ledger) HasCompleted(idempotencyKey string) bool {
	if idempotencyKey == "" {
		return false // Empty key = no dedupe
	}

	var exists int
	err := l.db.QueryRow(`
		SELECT 1 FROM event_ledger 
		WHERE idempotency_key = ? AND event_type = ?
		LIMIT 1
	`, idempotencyKey, string(EventActionCompleted)).Scan(&exists)

	return err == nil && exists == 1
}

// GetStarted returns the action_started entry for a given idempotency_key, if it exists
// Used for restart recovery of orphaned actions
func (l *Ledger) GetStarted(idempotencyKey string) (*Entry, error) {
	if idempotencyKey == "" {
		return nil, nil
	}

	var entry Entry
	var payloadStr sql.NullString
	var source, defID sql.NullString
	var timestamp int64

	err := l.db.QueryRow(`
		SELECT id, event_type, timestamp, payload, source, idempotency_key, def_id
		FROM event_ledger
		WHERE idempotency_key = ? AND event_type = ?
		ORDER BY id DESC LIMIT 1
	`, idempotencyKey, string(EventActionStarted)).Scan(
		&entry.ID, &entry.EventType, &timestamp, &payloadStr, &source, &entry.IdempotencyKey, &defID,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	entry.Timestamp = time.Unix(timestamp, 0).UTC()
	if source.Valid {
		entry.Source = source.String
	}
	if defID.Valid {
		entry.DefID = defID.String
	}

	if payloadStr.Valid && payloadStr.String != "" {
		if err := json.Unmarshal([]byte(payloadStr.String), &entry.Payload); err != nil {
			return nil, fmt.Errorf("failed to unmarshal payload: %w", err)
		}
	}

	return &entry, nil
}

// GetOrphanedStarts returns all action_started entries without corresponding action_completed
// Used for startup recovery of crashed actions
func (l *Ledger) GetOrphanedStarts() ([]*Entry, error) {
	rows, err := l.db.Query(`
		SELECT l1.id, l1.event_type, l1.timestamp, l1.payload, l1.source, l1.idempotency_key, l1.def_id
		FROM event_ledger l1
		WHERE l1.event_type = ?
		AND l1.idempotency_key IS NOT NULL
		AND l1.idempotency_key != ''
		AND NOT EXISTS (
			SELECT 1 FROM event_ledger l2 
			WHERE l2.idempotency_key = l1.idempotency_key 
			AND l2.event_type = ?
		)
		ORDER BY l1.id ASC
	`, string(EventActionStarted), string(EventActionCompleted))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return l.scanEntries(rows)
}

// GetLastCompletedForDef returns the timestamp of the last completed action for a specific definition
// Used for per-definition misfire detection
func (l *Ledger) GetLastCompletedForDef(defID string) (time.Time, bool) {
	var timestamp int64
	err := l.db.QueryRow(`
		SELECT MAX(timestamp) FROM event_ledger
		WHERE def_id = ? AND event_type = ?
	`, defID, string(EventActionCompleted)).Scan(&timestamp)

	if err != nil || timestamp == 0 {
		return time.Time{}, false
	}
	return time.Unix(timestamp, 0).UTC(), true
}

// GetByType returns entries filtered by event type
func (l *Ledger) GetByType(eventType EventType, limit int) ([]*Entry, error) {
	rows, err := l.db.Query(`
		SELECT id, event_type, timestamp, payload, source, idempotency_key, def_id
		FROM event_ledger
		WHERE event_type = ?
		ORDER BY timestamp DESC
		LIMIT ?
	`, string(eventType), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return l.scanEntries(rows)
}

// GetByTimeRange returns entries within a time range
func (l *Ledger) GetByTimeRange(start, end time.Time, limit int) ([]*Entry, error) {
	rows, err := l.db.Query(`
		SELECT id, event_type, timestamp, payload, source, idempotency_key, def_id
		FROM event_ledger
		WHERE timestamp >= ? AND timestamp <= ?
		ORDER BY timestamp DESC
		LIMIT ?
	`, start.Unix(), end.Unix(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return l.scanEntries(rows)
}

// DeleteOlderThan removes entries older than the specified duration (retention policy)
func (l *Ledger) DeleteOlderThan(retention time.Duration) (int64, error) {
	cutoff := time.Now().Add(-retention).Unix()
	result, err := l.db.Exec(`
		DELETE FROM event_ledger WHERE timestamp < ?
	`, cutoff)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (l *Ledger) scanEntries(rows *sql.Rows) ([]*Entry, error) {
	var entries []*Entry
	for rows.Next() {
		var entry Entry
		var payloadStr sql.NullString
		var source, defID, idempotencyKey sql.NullString
		var timestamp int64

		err := rows.Scan(
			&entry.ID, &entry.EventType, &timestamp, &payloadStr, &source, &idempotencyKey, &defID,
		)
		if err != nil {
			return nil, err
		}

		entry.Timestamp = time.Unix(timestamp, 0).UTC()
		if source.Valid {
			entry.Source = source.String
		}
		if defID.Valid {
			entry.DefID = defID.String
		}
		if idempotencyKey.Valid {
			entry.IdempotencyKey = idempotencyKey.String
		}

		if payloadStr.Valid && payloadStr.String != "" {
			entry.Payload = make(map[string]any)
			if err := json.Unmarshal([]byte(payloadStr.String), &entry.Payload); err != nil {
				return nil, fmt.Errorf("failed to unmarshal payload: %w", err)
			}
		}

		entries = append(entries, &entry)
	}

	return entries, rows.Err()
}
