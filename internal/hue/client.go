package hue

import (
	"context"
	"crypto/tls"
	"net/http"
	"time"

	"github.com/amimof/huego"
	"github.com/rs/zerolog/log"

	v2 "github.com/dokzlo13/lightd/internal/hue/v2"
)

// Client is a holder for Hue API clients with shared HTTP configuration.
// It provides access to both V1 (via huego) and V2 (via custom client) APIs.
type Client struct {
	v1 *huego.Bridge // V1 API via huego
	v2 *v2.Client    // V2 API via custom client (SSE support)
}

// NewClient creates a new Hue client holder.
//
// Note: huego (V1 client) uses http.DefaultClient internally and doesn't support
// custom HTTP clients. This is acceptable because:
// - V1 API uses HTTP (not HTTPS), so SSL verification isn't an issue
// - V1 requests are typically fast, so the default timeout is sufficient
//
// V2 client uses the custom HTTP client with TLS verification disabled
// (required for Hue bridge's self-signed certificates).
func NewClient(address, token string, timeout time.Duration) *Client {
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	// Create HTTP client for V2 with TLS verification disabled
	// (Hue bridge uses self-signed certificates)
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}

	httpClient := &http.Client{
		Timeout:   timeout,
		Transport: transport,
	}

	// Initialize huego bridge (uses http.DefaultClient internally)
	bridge := huego.New(address, token)

	// Initialize V2 client with custom HTTP client
	v2Client := v2.NewClient(address, token, httpClient)

	return &Client{
		v1: bridge,
		v2: v2Client,
	}
}

// Connect tests connectivity to both APIs
func (c *Client) Connect(ctx context.Context) error {
	// Test V1 API connection via huego
	if _, err := c.v1.GetCapabilities(); err != nil {
		return err
	}

	// Test V2 API connection
	if err := c.v2.Connect(ctx); err != nil {
		return err
	}

	log.Info().Str("address", c.v2.Address()).Msg("Connected to Hue bridge")
	return nil
}

// Close closes the V2 client connections
func (c *Client) Close() error {
	c.v2.Close()
	return nil
}

// V1 returns the huego bridge for direct V1 API access.
// Use this for all V1 operations (groups, scenes, lights via V1 API).
func (c *Client) V1() *huego.Bridge {
	return c.v1
}

// V2 returns the V2 client for direct V2 API access.
// Use this for SSE events and V2 light control.
func (c *Client) V2() *v2.Client {
	return c.v2
}

// Address returns the bridge address
func (c *Client) Address() string {
	return c.v2.Address()
}
