package state

import (
	"encoding/json"
	"fmt"
)

// TypedStore wraps Store with JSON marshaling for a specific type.
// Each resource provider uses its own TypedStore instance with its state struct.
type TypedStore[T any] struct {
	store *Store
	kind  string
}

// NewTypedStore creates a new typed store wrapper for the given kind.
func NewTypedStore[T any](store *Store, kind string) *TypedStore[T] {
	return &TypedStore[T]{
		store: store,
		kind:  kind,
	}
}

// Kind returns the resource kind this store handles.
func (s *TypedStore[T]) Kind() string {
	return s.kind
}

// Get retrieves and unmarshals the state for an ID.
// Returns zero value and version 0 if not found.
func (s *TypedStore[T]) Get(id string) (value T, version int64, err error) {
	payload, version, err := s.store.Get(s.kind, id)
	if err != nil {
		return value, 0, err
	}

	if payload == nil {
		return value, 0, nil
	}

	if err := json.Unmarshal(payload, &value); err != nil {
		return value, 0, fmt.Errorf("failed to unmarshal state: %w", err)
	}

	return value, version, nil
}

// Set marshals and stores the state for an ID.
func (s *TypedStore[T]) Set(id string, value T) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("failed to marshal state: %w", err)
	}

	return s.store.Set(s.kind, id, payload)
}

// GetDirty returns IDs where version > lastVersions[id].
func (s *TypedStore[T]) GetDirty(lastVersions map[string]int64) ([]string, error) {
	return s.store.GetDirty(s.kind, lastVersions)
}

// Delete removes the state for an ID.
func (s *TypedStore[T]) Delete(id string) error {
	return s.store.Delete(s.kind, id)
}

// Clear removes all state for this kind.
func (s *TypedStore[T]) Clear() error {
	return s.store.Clear(s.kind)
}

// GetAll retrieves all entries for this kind.
func (s *TypedStore[T]) GetAll() (map[string]T, map[string]int64, error) {
	payloads, versions, err := s.store.GetAll(s.kind)
	if err != nil {
		return nil, nil, err
	}

	values := make(map[string]T, len(payloads))
	for id, payload := range payloads {
		var value T
		if err := json.Unmarshal(payload, &value); err != nil {
			return nil, nil, fmt.Errorf("failed to unmarshal state for %s: %w", id, err)
		}
		values[id] = value
	}

	return values, versions, nil
}

// Update applies a modification function to the current state.
// If the ID doesn't exist, the modify function receives the zero value.
func (s *TypedStore[T]) Update(id string, modify func(current T) T) error {
	current, _, err := s.Get(id)
	if err != nil {
		return err
	}

	updated := modify(current)
	return s.Set(id, updated)
}
