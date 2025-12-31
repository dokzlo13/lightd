// Package ledger provides an append-only event history for HuePlanner.
// It supports action deduplication and auditing.
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
	EventActionCompleted EventType = "action_completed"
	EventActionFailed    EventType = "action_failed"
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
