package kv

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// SQLiteBucket is a persistent bucket backed by SQLite.
type SQLiteBucket struct {
	db   *sql.DB
	name string
}

// NewSQLiteBucket creates a new SQLite-backed bucket.
func NewSQLiteBucket(db *sql.DB, name string) *SQLiteBucket {
	return &SQLiteBucket{
		db:   db,
		name: name,
	}
}

// Name returns the bucket name.
func (b *SQLiteBucket) Name() string {
	return b.name
}

// IsPersistent returns true (SQLite buckets are always persistent).
func (b *SQLiteBucket) IsPersistent() bool {
	return true
}

// Store saves a value with the given key.
func (b *SQLiteBucket) Store(key string, value any, opts *StoreOptions) error {
	// Serialize value to JSON
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("failed to marshal value: %w", err)
	}

	now := time.Now().UTC().Unix()

	var expiresAt *int64
	if opts != nil && opts.TTL > 0 {
		exp := time.Now().Add(opts.TTL).UTC().Unix()
		expiresAt = &exp
	}

	_, err = b.db.Exec(`
		INSERT INTO kv_store (bucket, key, value, expires_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(bucket, key) DO UPDATE SET
			value = excluded.value,
			expires_at = excluded.expires_at,
			updated_at = excluded.updated_at
	`, b.name, key, string(data), expiresAt, now, now)

	if err != nil {
		return fmt.Errorf("failed to store value: %w", err)
	}

	return nil
}

// Get retrieves a value by key.
func (b *SQLiteBucket) Get(key string) (any, error) {
	var valueStr string
	var expiresAt sql.NullInt64

	err := b.db.QueryRow(`
		SELECT value, expires_at FROM kv_store
		WHERE bucket = ? AND key = ?
	`, b.name, key).Scan(&valueStr, &expiresAt)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get value: %w", err)
	}

	// Check expiry
	if expiresAt.Valid && time.Now().UTC().Unix() > expiresAt.Int64 {
		// Expired - delete and return nil
		_, _ = b.db.Exec(`DELETE FROM kv_store WHERE bucket = ? AND key = ?`, b.name, key)
		return nil, nil
	}

	// Unmarshal value
	var value any
	if err := json.Unmarshal([]byte(valueStr), &value); err != nil {
		return nil, fmt.Errorf("failed to unmarshal value: %w", err)
	}

	return value, nil
}

// Exists returns true if the key exists and hasn't expired.
func (b *SQLiteBucket) Exists(key string) (bool, error) {
	var expiresAt sql.NullInt64

	err := b.db.QueryRow(`
		SELECT expires_at FROM kv_store
		WHERE bucket = ? AND key = ?
	`, b.name, key).Scan(&expiresAt)

	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to check existence: %w", err)
	}

	// Check expiry
	if expiresAt.Valid && time.Now().UTC().Unix() > expiresAt.Int64 {
		// Expired - delete and return false
		_, _ = b.db.Exec(`DELETE FROM kv_store WHERE bucket = ? AND key = ?`, b.name, key)
		return false, nil
	}

	return true, nil
}

// Delete removes a key from the bucket.
func (b *SQLiteBucket) Delete(key string) (bool, error) {
	result, err := b.db.Exec(`
		DELETE FROM kv_store WHERE bucket = ? AND key = ?
	`, b.name, key)
	if err != nil {
		return false, fmt.Errorf("failed to delete key: %w", err)
	}

	affected, _ := result.RowsAffected()
	return affected > 0, nil
}

// Keys returns all non-expired keys in the bucket.
func (b *SQLiteBucket) Keys() ([]string, error) {
	now := time.Now().UTC().Unix()

	rows, err := b.db.Query(`
		SELECT key FROM kv_store
		WHERE bucket = ? AND (expires_at IS NULL OR expires_at > ?)
	`, b.name, now)
	if err != nil {
		return nil, fmt.Errorf("failed to list keys: %w", err)
	}
	defer rows.Close()

	var keys []string
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, fmt.Errorf("failed to scan key: %w", err)
		}
		keys = append(keys, key)
	}

	return keys, rows.Err()
}

// Clear removes all keys from the bucket.
func (b *SQLiteBucket) Clear() error {
	_, err := b.db.Exec(`DELETE FROM kv_store WHERE bucket = ?`, b.name)
	if err != nil {
		return fmt.Errorf("failed to clear bucket: %w", err)
	}
	return nil
}

// CleanupExpired removes all expired entries from the database.
// This is typically called periodically by the manager.
func CleanupExpired(db *sql.DB) (int64, error) {
	now := time.Now().UTC().Unix()

	result, err := db.Exec(`
		DELETE FROM kv_store WHERE expires_at IS NOT NULL AND expires_at <= ?
	`, now)
	if err != nil {
		return 0, fmt.Errorf("failed to cleanup expired entries: %w", err)
	}

	return result.RowsAffected()
}
