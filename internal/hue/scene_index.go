package hue

import (
	"fmt"
	"sync"

	"github.com/amimof/huego"
)

// SceneIndex provides efficient lookup for Hue scenes.
// It stores scenes once and provides O(1) lookup by name+group or ID.
// This is a pure storage - caller is responsible for fetching and loading data.
type SceneIndex struct {
	mu        sync.RWMutex
	scenes    []huego.Scene  // source of truth, stored once
	byNameKey map[string]int // "groupID:name" -> index into scenes
	byID      map[string]int // sceneID -> index into scenes
}

// NewSceneIndex creates a new empty scene index.
func NewSceneIndex() *SceneIndex {
	return &SceneIndex{
		byNameKey: make(map[string]int),
		byID:      make(map[string]int),
	}
}

// Load populates the index with scenes.
// This replaces any existing data.
func (s *SceneIndex) Load(scenes []huego.Scene) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.scenes = scenes
	s.byNameKey = make(map[string]int, len(scenes))
	s.byID = make(map[string]int, len(scenes))

	for i := range scenes {
		// Index by groupID:name
		key := scenes[i].Group + ":" + scenes[i].Name
		s.byNameKey[key] = i

		// Index by ID
		s.byID[scenes[i].ID] = i
	}
}

// FindByName looks up a scene by name and group ID.
// Returns error if not found.
func (s *SceneIndex) FindByName(name, groupID string) (*huego.Scene, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	key := groupID + ":" + name
	idx, ok := s.byNameKey[key]
	if !ok {
		return nil, fmt.Errorf("scene '%s' not found in group '%s'", name, groupID)
	}

	return &s.scenes[idx], nil
}

// FindByID looks up a scene by its ID.
// Returns error if not found.
func (s *SceneIndex) FindByID(sceneID string) (*huego.Scene, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	idx, ok := s.byID[sceneID]
	if !ok {
		return nil, fmt.Errorf("scene '%s' not found", sceneID)
	}

	return &s.scenes[idx], nil
}

// GetAll returns all indexed scenes.
func (s *SceneIndex) GetAll() []huego.Scene {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Return a copy to prevent external mutation
	result := make([]huego.Scene, len(s.scenes))
	copy(result, s.scenes)
	return result
}

// Count returns the number of indexed scenes.
func (s *SceneIndex) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.scenes)
}

// Clear removes all indexed data.
func (s *SceneIndex) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.scenes = nil
	s.byNameKey = make(map[string]int)
	s.byID = make(map[string]int)
}

