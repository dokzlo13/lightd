// Package kv provides a key-value storage system with SQLite persistence and in-memory options.
package kv

import "time"

// Value represents a stored value with metadata.
type Value struct {
	Data      any       // The actual value (string, number, bool, or map)
	ExpiresAt time.Time // Zero value means no expiry
	CreatedAt time.Time
	UpdatedAt time.Time
}

// IsExpired returns true if the value has expired.
func (v *Value) IsExpired() bool {
	if v.ExpiresAt.IsZero() {
		return false
	}
	return time.Now().After(v.ExpiresAt)
}

// StoreOptions contains optional parameters for Store operations.
type StoreOptions struct {
	TTL time.Duration // Time-to-live; zero means no expiry
}

// Bucket is the interface for key-value storage operations.
type Bucket interface {
	// Name returns the bucket name.
	Name() string

	// IsPersistent returns true if the bucket is backed by SQLite.
	IsPersistent() bool

	// Store saves a value with the given key.
	// The value can be a string, number, boolean, or map.
	// Options can specify TTL for automatic expiry.
	Store(key string, value any, opts *StoreOptions) error

	// Get retrieves a value by key.
	// Returns nil if the key doesn't exist or has expired.
	Get(key string) (any, error)

	// Exists returns true if the key exists and hasn't expired.
	Exists(key string) (bool, error)

	// Delete removes a key from the bucket.
	// Returns true if the key existed.
	Delete(key string) (bool, error)

	// Keys returns all non-expired keys in the bucket.
	Keys() ([]string, error)

	// Clear removes all keys from the bucket.
	Clear() error
}

