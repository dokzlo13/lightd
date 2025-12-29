package hue

import (
	"fmt"
	"sync"

	"github.com/amimof/huego"
	"github.com/rs/zerolog/log"
)

// SceneCache provides cached access to Hue scenes.
// It fetches scenes from the bridge on first access and caches them.
type SceneCache struct {
	bridge *huego.Bridge
	mu     sync.RWMutex
	scenes []huego.Scene
	loaded bool
}

// NewSceneCache creates a new scene cache
func NewSceneCache(bridge *huego.Bridge) *SceneCache {
	return &SceneCache{
		bridge: bridge,
	}
}

// GetScenes returns all scenes, fetching from bridge if not cached
func (c *SceneCache) GetScenes() ([]huego.Scene, error) {
	c.mu.RLock()
	if c.loaded {
		scenes := c.scenes
		c.mu.RUnlock()
		return scenes, nil
	}
	c.mu.RUnlock()

	return c.Refresh()
}

// Refresh fetches scenes from bridge and updates cache
func (c *SceneCache) Refresh() ([]huego.Scene, error) {
	scenes, err := c.bridge.GetScenes()
	if err != nil {
		return nil, fmt.Errorf("failed to fetch scenes: %w", err)
	}

	c.mu.Lock()
	c.scenes = scenes
	c.loaded = true
	c.mu.Unlock()

	log.Debug().Int("count", len(scenes)).Msg("Scene cache refreshed")
	return scenes, nil
}

// FindByName finds a scene by name and group ID
func (c *SceneCache) FindByName(name, groupID string) (*huego.Scene, error) {
	scenes, err := c.GetScenes()
	if err != nil {
		return nil, err
	}

	for i := range scenes {
		if scenes[i].Name == name && scenes[i].Group == groupID {
			return &scenes[i], nil
		}
	}

	return nil, fmt.Errorf("scene '%s' not found in group '%s'", name, groupID)
}

// FindByID finds a scene by ID
func (c *SceneCache) FindByID(sceneID string) (*huego.Scene, error) {
	scenes, err := c.GetScenes()
	if err != nil {
		return nil, err
	}

	for i := range scenes {
		if scenes[i].ID == sceneID {
			return &scenes[i], nil
		}
	}

	return nil, fmt.Errorf("scene '%s' not found", sceneID)
}

// Clear clears the cache, forcing a refresh on next access
func (c *SceneCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.scenes = nil
	c.loaded = false
}
