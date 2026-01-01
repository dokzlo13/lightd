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
	// Event ledger - append-only history for dedupe and auditing
	// NO unique constraint - we log multiple events per occurrence (started, completed, failed)
	_, err := db.Exec(`
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

	// Resource state - generic JSON state store keyed by (kind, id)
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS resource_state (
			kind TEXT NOT NULL,
			id TEXT NOT NULL,
			payload TEXT NOT NULL,
			version INTEGER DEFAULT 1,
			updated_at INTEGER NOT NULL,
			PRIMARY KEY (kind, id)
		);
		CREATE INDEX IF NOT EXISTS idx_resource_state_kind ON resource_state(kind);
	`)
	if err != nil {
		return fmt.Errorf("failed to create resource_state table: %w", err)
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

	// KV store - generic key-value storage with optional TTL
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS kv_store (
			bucket TEXT NOT NULL,
			key TEXT NOT NULL,
			value TEXT NOT NULL,
			expires_at INTEGER,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL,
			PRIMARY KEY (bucket, key)
		);
		CREATE INDEX IF NOT EXISTS idx_kv_bucket ON kv_store(bucket);
		CREATE INDEX IF NOT EXISTS idx_kv_expires ON kv_store(expires_at) WHERE expires_at IS NOT NULL;
	`)
	if err != nil {
		return fmt.Errorf("failed to create kv_store table: %w", err)
	}

	return nil
}

// Close closes the database connection
func (db *DB) Close() error {
	return db.DB.Close()
}
