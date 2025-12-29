package hue

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// Client provides unified access to both Hue v1 and v2 APIs
type Client struct {
	address    string
	token      string
	httpClient *http.Client
	mu         sync.RWMutex

	// Cached data
	scenes   []Scene
	scenesV2 []SceneV2
	groups   []Group
}

// NewClient creates a new Hue client
func NewClient(address, token string, timeout time.Duration) *Client {
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	// Create HTTP client that ignores TLS verification (Hue bridge uses self-signed cert)
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}

	return &Client{
		address: address,
		token:   token,
		httpClient: &http.Client{
			Timeout:   timeout,
			Transport: transport,
		},
	}
}

// Connect establishes connection to the Hue bridge
func (c *Client) Connect(ctx context.Context) error {
	// Test v1 API connection
	resp, err := c.v1Request(ctx, "GET", "capabilities", nil)
	if err != nil {
		return fmt.Errorf("failed to connect to Hue bridge v1 API: %w", err)
	}
	resp.Body.Close()

	// Test v2 API connection
	resp, err = c.v2Request(ctx, "GET", "resource", nil)
	if err != nil {
		return fmt.Errorf("failed to connect to Hue bridge v2 API: %w", err)
	}
	resp.Body.Close()

	// Preload scenes and groups
	if err := c.refreshCache(ctx); err != nil {
		log.Warn().Err(err).Msg("Failed to preload cache")
	}

	log.Info().Str("address", c.address).Msg("Connected to Hue bridge")
	return nil
}

// Close closes the client
func (c *Client) Close() error {
	c.httpClient.CloseIdleConnections()
	return nil
}

func (c *Client) v1URL(path string) string {
	return fmt.Sprintf("http://%s/api/%s/%s", c.address, c.token, path)
}

func (c *Client) v2URL(path string) string {
	return fmt.Sprintf("https://%s/clip/v2/%s", c.address, path)
}

func (c *Client) v1Request(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.v1URL(path), body)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.httpClient.Do(req)
}

func (c *Client) v2Request(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.v2URL(path), body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("hue-application-key", c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.httpClient.Do(req)
}

func (c *Client) refreshCache(ctx context.Context) error {
	scenes, err := c.fetchScenes(ctx)
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.scenes = scenes
	c.mu.Unlock()

	scenesV2, err := c.fetchScenesV2(ctx)
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.scenesV2 = scenesV2
	c.mu.Unlock()

	groups, err := c.fetchGroups(ctx)
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.groups = groups
	c.mu.Unlock()

	log.Debug().
		Int("scenes_v1", len(scenes)).
		Int("scenes_v2", len(scenesV2)).
		Int("groups", len(groups)).
		Msg("Cache refreshed")

	return nil
}

// GetGroup returns a group by ID (v1 API)
func (c *Client) GetGroup(ctx context.Context, groupID string) (*Group, error) {
	resp, err := c.v1Request(ctx, "GET", fmt.Sprintf("groups/%s", groupID), nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var group Group
	if err := json.NewDecoder(resp.Body).Decode(&group); err != nil {
		return nil, err
	}
	group.ID = groupID

	return &group, nil
}

// GetScenes returns all scenes (v1 API)
func (c *Client) GetScenes(ctx context.Context) ([]Scene, error) {
	c.mu.RLock()
	if len(c.scenes) > 0 {
		scenes := c.scenes
		c.mu.RUnlock()
		return scenes, nil
	}
	c.mu.RUnlock()

	return c.fetchScenes(ctx)
}

func (c *Client) fetchScenes(ctx context.Context) ([]Scene, error) {
	resp, err := c.v1Request(ctx, "GET", "scenes", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var raw map[string]Scene
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}

	scenes := make([]Scene, 0, len(raw))
	for id, scene := range raw {
		scene.ID = id
		scenes = append(scenes, scene)
	}

	return scenes, nil
}

func (c *Client) fetchGroups(ctx context.Context) ([]Group, error) {
	resp, err := c.v1Request(ctx, "GET", "groups", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var raw map[string]Group
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}

	groups := make([]Group, 0, len(raw))
	for id, group := range raw {
		group.ID = id
		groups = append(groups, group)
	}

	return groups, nil
}

// GetScenesV2 returns all scenes (v2 API)
func (c *Client) GetScenesV2(ctx context.Context) ([]SceneV2, error) {
	c.mu.RLock()
	if len(c.scenesV2) > 0 {
		scenes := c.scenesV2
		c.mu.RUnlock()
		return scenes, nil
	}
	c.mu.RUnlock()

	return c.fetchScenesV2(ctx)
}

func (c *Client) fetchScenesV2(ctx context.Context) ([]SceneV2, error) {
	resp, err := c.v2Request(ctx, "GET", "resource/scene", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result struct {
		Data []SceneV2 `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return result.Data, nil
}

// FindSceneByName finds a scene by name and group
func (c *Client) FindSceneByName(ctx context.Context, name, groupID string) (*Scene, error) {
	scenes, err := c.GetScenes(ctx)
	if err != nil {
		return nil, err
	}

	for _, scene := range scenes {
		if scene.Name == name && scene.Group == groupID {
			return &scene, nil
		}
	}

	return nil, fmt.Errorf("scene '%s' not found in group '%s'", name, groupID)
}

// FindSceneV2ByName finds a v2 scene by name
func (c *Client) FindSceneV2ByName(ctx context.Context, name string) (*SceneV2, error) {
	scenes, err := c.GetScenesV2(ctx)
	if err != nil {
		return nil, err
	}

	for _, scene := range scenes {
		if scene.Metadata.Name == name {
			return &scene, nil
		}
	}

	return nil, fmt.Errorf("scene '%s' not found", name)
}

// ActivateScene activates a scene (v1 API)
func (c *Client) ActivateScene(ctx context.Context, groupID, sceneID string) error {
	body := strings.NewReader(fmt.Sprintf(`{"scene":"%s"}`, sceneID))
	resp, err := c.v1Request(ctx, "PUT", fmt.Sprintf("groups/%s/action", groupID), body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to activate scene: %s", string(body))
	}

	log.Debug().
		Str("group", groupID).
		Str("scene", sceneID).
		Msg("Scene activated")

	return nil
}

// SetGroupAction sends an action to a group (v1 API)
func (c *Client) SetGroupAction(ctx context.Context, groupID string, action map[string]interface{}) error {
	bodyBytes, err := json.Marshal(action)
	if err != nil {
		return err
	}

	resp, err := c.v1Request(ctx, "PUT", fmt.Sprintf("groups/%s/action", groupID), strings.NewReader(string(bodyBytes)))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to set group action: %s", string(body))
	}

	return nil
}

// GetLight returns a light by ID (v2 API)
func (c *Client) GetLight(ctx context.Context, lightID string) (*Light, error) {
	resp, err := c.v2Request(ctx, "GET", fmt.Sprintf("resource/light/%s", lightID), nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result struct {
		Data []Light `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	if len(result.Data) == 0 {
		return nil, fmt.Errorf("light '%s' not found", lightID)
	}

	return &result.Data[0], nil
}

// UpdateLight updates a light (v2 API)
func (c *Client) UpdateLight(ctx context.Context, lightID string, update map[string]interface{}) error {
	bodyBytes, err := json.Marshal(update)
	if err != nil {
		return err
	}

	resp, err := c.v2Request(ctx, "PUT", fmt.Sprintf("resource/light/%s", lightID), strings.NewReader(string(bodyBytes)))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to update light: %s", string(body))
	}

	return nil
}

// RefreshCache forces a cache refresh
func (c *Client) RefreshCache(ctx context.Context) error {
	return c.refreshCache(ctx)
}

// Address returns the bridge address
func (c *Client) Address() string {
	return c.address
}
