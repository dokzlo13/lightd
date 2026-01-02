package kv

import (
	"sync"
	"time"
)

// memoryEntry holds a value with its metadata in memory.
type memoryEntry struct {
	value     any
	expiresAt time.Time // Zero value means no expiry
	createdAt time.Time
	updatedAt time.Time
}

// isExpired returns true if the entry has expired.
func (e *memoryEntry) isExpired() bool {
	if e.expiresAt.IsZero() {
		return false
	}
	return time.Now().After(e.expiresAt)
}

// MemoryBucket is an in-memory bucket (not persisted).
type MemoryBucket struct {
	name    string
	entries map[string]*memoryEntry
	mu      sync.RWMutex
}

// NewMemoryBucket creates a new in-memory bucket.
func NewMemoryBucket(name string) *MemoryBucket {
	return &MemoryBucket{
		name:    name,
		entries: make(map[string]*memoryEntry),
	}
}

// Name returns the bucket name.
func (b *MemoryBucket) Name() string {
	return b.name
}

// IsPersistent returns false (memory buckets are not persistent).
func (b *MemoryBucket) IsPersistent() bool {
	return false
}

// Store saves a value with the given key.
func (b *MemoryBucket) Store(key string, value any, opts *StoreOptions) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	now := time.Now()

	entry := &memoryEntry{
		value:     value,
		createdAt: now,
		updatedAt: now,
	}

	if opts != nil && opts.TTL > 0 {
		entry.expiresAt = now.Add(opts.TTL)
	}

	// Preserve created_at if updating existing entry
	if existing, ok := b.entries[key]; ok && !existing.isExpired() {
		entry.createdAt = existing.createdAt
	}

	b.entries[key] = entry
	return nil
}

// Get retrieves a value by key.
func (b *MemoryBucket) Get(key string) (any, error) {
	b.mu.RLock()
	entry, ok := b.entries[key]
	b.mu.RUnlock()

	if !ok {
		return nil, nil
	}

	if entry.isExpired() {
		// Lazy deletion of expired entry
		b.mu.Lock()
		delete(b.entries, key)
		b.mu.Unlock()
		return nil, nil
	}

	return entry.value, nil
}

// Exists returns true if the key exists and hasn't expired.
func (b *MemoryBucket) Exists(key string) (bool, error) {
	b.mu.RLock()
	entry, ok := b.entries[key]
	b.mu.RUnlock()

	if !ok {
		return false, nil
	}

	if entry.isExpired() {
		// Lazy deletion of expired entry
		b.mu.Lock()
		delete(b.entries, key)
		b.mu.Unlock()
		return false, nil
	}

	return true, nil
}

// Delete removes a key from the bucket.
func (b *MemoryBucket) Delete(key string) (bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	_, ok := b.entries[key]
	if ok {
		delete(b.entries, key)
	}
	return ok, nil
}

// Keys returns all non-expired keys in the bucket.
func (b *MemoryBucket) Keys() ([]string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	var keys []string
	var expiredKeys []string

	for key, entry := range b.entries {
		if entry.isExpired() {
			expiredKeys = append(expiredKeys, key)
		} else {
			keys = append(keys, key)
		}
	}

	// Clean up expired entries
	for _, key := range expiredKeys {
		delete(b.entries, key)
	}

	return keys, nil
}

// Clear removes all keys from the bucket.
func (b *MemoryBucket) Clear() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.entries = make(map[string]*memoryEntry)
	return nil
}

// CleanupExpired removes all expired entries from the bucket.
// Returns the number of entries removed.
func (b *MemoryBucket) CleanupExpired() int {
	b.mu.Lock()
	defer b.mu.Unlock()

	count := 0
	for key, entry := range b.entries {
		if entry.isExpired() {
			delete(b.entries, key)
			count++
		}
	}
	return count
}

