// Package db provides a centralized database connection and schema for HuePlanner.
package db

import (
	"database/sql"
	"fmt"

	_ "github.com/mattn/go-sqlite3"
)

// DB wraps the SQLite database connection
type DB struct {
	*sql.DB
}

// Open opens the database and initializes the schema
func Open(dbPath string) (*DB, error) {
	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL")
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	if err := initSchema(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	return &DB{db}, nil
}

// initSchema creates all required tables
func initSchema(db *sql.DB) error {
	// Schedule definitions - stable rules that survive restarts
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS schedule_definitions (
			id TEXT PRIMARY KEY,
			time_expr TEXT NOT NULL,
			action_name TEXT NOT NULL,
			action_args TEXT,
			tag TEXT,
			misfire_policy TEXT DEFAULT 'run_latest',
			enabled INTEGER DEFAULT 1,
			created_at INTEGER NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_schedule_definitions_tag ON schedule_definitions(tag);
		CREATE INDEX IF NOT EXISTS idx_schedule_definitions_enabled ON schedule_definitions(enabled);
	`)
	if err != nil {
		return fmt.Errorf("failed to create schedule_definitions table: %w", err)
	}

	// Schedule occurrences - computed cache of upcoming occurrences
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS schedule_occurrences (
			def_id TEXT NOT NULL,
			occurrence_id TEXT NOT NULL,
			run_at INTEGER NOT NULL,
			is_next INTEGER DEFAULT 0,
			PRIMARY KEY (def_id, occurrence_id),
			FOREIGN KEY (def_id) REFERENCES schedule_definitions(id) ON DELETE CASCADE
		);
		CREATE INDEX IF NOT EXISTS idx_occurrences_run_at ON schedule_occurrences(run_at);
		CREATE INDEX IF NOT EXISTS idx_occurrences_is_next ON schedule_occurrences(is_next);
	`)
	if err != nil {
		return fmt.Errorf("failed to create schedule_occurrences table: %w", err)
	}

	// Event ledger - append-only history for dedupe and auditing
	// NO unique constraint - we log multiple events per occurrence (started, completed, failed)
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS event_ledger (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			event_type TEXT NOT NULL,
			timestamp INTEGER NOT NULL,
			payload TEXT,
			source TEXT,
			idempotency_key TEXT,
			def_id TEXT
		);
		CREATE INDEX IF NOT EXISTS idx_ledger_type_ts ON event_ledger(event_type, timestamp);
		CREATE INDEX IF NOT EXISTS idx_ledger_idempotency ON event_ledger(idempotency_key, event_type);
	`)
	if err != nil {
		return fmt.Errorf("failed to create event_ledger table: %w", err)
	}

	// Partial index for efficient per-definition misfire queries
	_, err = db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_ledger_def_completed 
		ON event_ledger(def_id, event_type, timestamp) 
		WHERE def_id IS NOT NULL AND event_type = 'action_completed';
	`)
	if err != nil {
		return fmt.Errorf("failed to create idx_ledger_def_completed index: %w", err)
	}

	// Unique partial index for idempotency: only one action_completed per idempotency_key
	// This ensures "first writer wins" and prevents duplicate completions
	_, err = db.Exec(`
		CREATE UNIQUE INDEX IF NOT EXISTS idx_ledger_idempotency_completed 
		ON event_ledger(idempotency_key) 
		WHERE idempotency_key IS NOT NULL AND idempotency_key != '' AND event_type = 'action_completed';
	`)
	if err != nil {
		return fmt.Errorf("failed to create idx_ledger_idempotency_completed index: %w", err)
	}

	// Desired state - per-group state with versioning for dirty tracking
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS desired_state (
			group_id TEXT PRIMARY KEY,
			bank_scene_name TEXT,
			desired_power INTEGER,
			version INTEGER DEFAULT 0,
			updated_at INTEGER NOT NULL
		);
	`)
	if err != nil {
		return fmt.Errorf("failed to create desired_state table: %w", err)
	}

	// Geocache - persisted location lookups to avoid repeated Nominatim calls
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS geocache (
			query TEXT PRIMARY KEY,
			display_name TEXT NOT NULL,
			latitude REAL NOT NULL,
			longitude REAL NOT NULL,
			created_at INTEGER NOT NULL
		);
	`)
	if err != nil {
		return fmt.Errorf("failed to create geocache table: %w", err)
	}

	return nil
}

// Close closes the database connection
func (db *DB) Close() error {
	return db.DB.Close()
}
