package v2

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Client provides access to Hue V2 API (CLIP API).
// This client is HTTP-only with no caching - pure transport layer.
// Used for SSE events and individual light control.
type Client struct {
	address    string
	token      string
	httpClient *http.Client
}

// NewClient creates a new V2 API client.
// The httpClient should have TLS verification disabled for Hue bridge's self-signed cert.
func NewClient(address, token string, httpClient *http.Client) *Client {
	return &Client{
		address:    address,
		token:      token,
		httpClient: httpClient,
	}
}

// Address returns the bridge address
func (c *Client) Address() string {
	return c.address
}

// Token returns the application key (for SSE)
func (c *Client) Token() string {
	return c.token
}

// Close closes idle connections
func (c *Client) Close() {
	c.httpClient.CloseIdleConnections()
}

// Connect tests connectivity to the V2 API
func (c *Client) Connect(ctx context.Context) error {
	resp, err := c.Request(ctx, "GET", "resource", nil)
	if err != nil {
		return fmt.Errorf("failed to connect to Hue bridge V2 API: %w", err)
	}
	resp.Body.Close()
	return nil
}

func (c *Client) url(path string) string {
	return fmt.Sprintf("https://%s/clip/v2/%s", c.address, path)
}

// Request performs an HTTP request to the V2 API
func (c *Client) Request(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.url(path), body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("hue-application-key", c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.httpClient.Do(req)
}

// GetLight returns a light by ID
func (c *Client) GetLight(ctx context.Context, lightID string) (*Light, error) {
	resp, err := c.Request(ctx, "GET", fmt.Sprintf("resource/light/%s", lightID), nil)
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

// GetLights returns all lights
func (c *Client) GetLights(ctx context.Context) ([]Light, error) {
	resp, err := c.Request(ctx, "GET", "resource/light", nil)
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

	return result.Data, nil
}

// UpdateLight updates a light
func (c *Client) UpdateLight(ctx context.Context, lightID string, update map[string]interface{}) error {
	bodyBytes, err := json.Marshal(update)
	if err != nil {
		return err
	}

	resp, err := c.Request(ctx, "PUT", fmt.Sprintf("resource/light/%s", lightID), strings.NewReader(string(bodyBytes)))
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

// GetScenes returns all scenes
func (c *Client) GetScenes(ctx context.Context) ([]Scene, error) {
	resp, err := c.Request(ctx, "GET", "resource/scene", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result struct {
		Data []Scene `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return result.Data, nil
}
