package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Helper to create a bool pointer
func boolPtr(b bool) *bool {
	return &b
}

// Helper to create a valid minimal config for testing
func validConfig(t *testing.T) *Config {
	t.Helper()
	// Create a temp script file
	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "test.lua")
	if err := os.WriteFile(scriptPath, []byte("-- test"), 0644); err != nil {
		t.Fatal(err)
	}

	// Disable scheduler/geo by default to avoid location validation
	// Tests that need geo enabled should explicitly enable it
	return &Config{
		Hue: HueConfig{
			Bridge: "192.168.1.1",
			Token:  "test-token-12345",
		},
		Script: scriptPath,
		Events: EventsConfig{
			Scheduler: SchedulerConfig{
				Enabled: boolPtr(false),
			},
		},
	}
}

func TestConfigValidate_Valid(t *testing.T) {
	cfg := validConfig(t)
	if err := cfg.Validate(); err != nil {
		t.Errorf("valid config should not error: %v", err)
	}
}

func TestConfigValidate_MissingHueBridge(t *testing.T) {
	cfg := validConfig(t)
	cfg.Hue.Bridge = ""

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for missing bridge")
	}

	if !strings.Contains(err.Error(), "hue.bridge") {
		t.Errorf("error should mention hue.bridge: %v", err)
	}
}

func TestConfigValidate_MissingHueToken(t *testing.T) {
	cfg := validConfig(t)
	cfg.Hue.Token = ""

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for missing token")
	}

	if !strings.Contains(err.Error(), "hue.token") {
		t.Errorf("error should mention hue.token: %v", err)
	}
}

func TestConfigValidate_MissingScript(t *testing.T) {
	cfg := validConfig(t)
	cfg.Script = "/nonexistent/path/script.lua"

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for missing script")
	}

	if !strings.Contains(err.Error(), "script") {
		t.Errorf("error should mention script: %v", err)
	}
}

func TestConfigValidate_NegativeTimeout(t *testing.T) {
	cfg := validConfig(t)
	cfg.Hue.Timeout = Duration(-1 * time.Second)

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for negative timeout")
	}

	if !strings.Contains(err.Error(), "hue.timeout") {
		t.Errorf("error should mention hue.timeout: %v", err)
	}
}

func TestConfigValidate_InvalidLogLevel(t *testing.T) {
	cfg := validConfig(t)
	cfg.Log.Level = "invalid"

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for invalid log level")
	}

	if !strings.Contains(err.Error(), "log.level") {
		t.Errorf("error should mention log.level: %v", err)
	}
}

func TestConfigValidate_ValidLogLevels(t *testing.T) {
	validLevels := []string{"", "trace", "debug", "info", "warn", "error", "fatal", "panic"}

	for _, level := range validLevels {
		t.Run(level, func(t *testing.T) {
			cfg := validConfig(t)
			cfg.Log.Level = level

			err := cfg.Validate()
			if err != nil {
				t.Errorf("log level %q should be valid: %v", level, err)
			}
		})
	}
}

func TestConfigValidate_InvalidPort(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*Config)
		field string
	}{
		{
			name: "healthcheck port too high",
			setup: func(c *Config) {
				c.Healthcheck.Enabled = true
				c.Healthcheck.Port = 70000
			},
			field: "healthcheck.port",
		},
		{
			name: "healthcheck port zero is ok (uses default)",
			setup: func(c *Config) {
				c.Healthcheck.Enabled = true
				c.Healthcheck.Port = 0
			},
			field: "", // No error expected
		},
		{
			name: "webhook port too high",
			setup: func(c *Config) {
				c.Events.Webhook.Enabled = true
				c.Events.Webhook.Port = 70000
			},
			field: "events.webhook.port",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig(t)
			tt.setup(cfg)

			err := cfg.Validate()
			if tt.field == "" {
				if err != nil {
					t.Errorf("expected no error, got: %v", err)
				}
				return
			}

			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tt.field) {
				t.Errorf("error should mention %s: %v", tt.field, err)
			}
		})
	}
}

func TestConfigValidate_GeoEnabled_NoLocation(t *testing.T) {
	cfg := validConfig(t)
	cfg.Events.Scheduler.Enabled = boolPtr(true)
	cfg.Events.Scheduler.Geo.Enabled = boolPtr(true)
	cfg.Events.Scheduler.Geo.Location = ""
	cfg.Events.Scheduler.Geo.Lat = 0
	cfg.Events.Scheduler.Geo.Lon = 0

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for geo enabled but no location")
	}

	if !strings.Contains(err.Error(), "geo") {
		t.Errorf("error should mention geo: %v", err)
	}
}

func TestConfigValidate_GeoDisabled_NoLocation_OK(t *testing.T) {
	cfg := validConfig(t)
	cfg.Events.Scheduler.Enabled = boolPtr(true)
	cfg.Events.Scheduler.Geo.Enabled = boolPtr(false)
	cfg.Events.Scheduler.Geo.Location = ""

	err := cfg.Validate()
	if err != nil {
		t.Errorf("geo disabled should not require location: %v", err)
	}
}

func TestConfigValidate_SchedulerDisabled_NoLocation_OK(t *testing.T) {
	cfg := validConfig(t)
	cfg.Events.Scheduler.Enabled = boolPtr(false)
	cfg.Events.Scheduler.Geo.Location = ""

	err := cfg.Validate()
	if err != nil {
		t.Errorf("scheduler disabled should not require location: %v", err)
	}
}

func TestConfigValidate_GeoWithLocation(t *testing.T) {
	cfg := validConfig(t)
	cfg.Events.Scheduler.Enabled = boolPtr(true)
	cfg.Events.Scheduler.Geo.Enabled = boolPtr(true)
	cfg.Events.Scheduler.Geo.Location = "Helsinki, Finland"

	err := cfg.Validate()
	if err != nil {
		t.Errorf("geo with location should be valid: %v", err)
	}
}

func TestConfigValidate_GeoWithCoordinates(t *testing.T) {
	cfg := validConfig(t)
	cfg.Events.Scheduler.Enabled = boolPtr(true)
	cfg.Events.Scheduler.Geo.Enabled = boolPtr(true)
	cfg.Events.Scheduler.Geo.Lat = 60.1699
	cfg.Events.Scheduler.Geo.Lon = 24.9384

	err := cfg.Validate()
	if err != nil {
		t.Errorf("geo with coordinates should be valid: %v", err)
	}
}

func TestConfigValidate_InvalidLatitude(t *testing.T) {
	tests := []struct {
		name string
		lat  float64
	}{
		{"lat too low", -91},
		{"lat too high", 91},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig(t)
			cfg.Events.Scheduler.Enabled = boolPtr(true)
			cfg.Events.Scheduler.Geo.Enabled = boolPtr(true)
			cfg.Events.Scheduler.Geo.Lat = tt.lat
			cfg.Events.Scheduler.Geo.Lon = 0

			err := cfg.Validate()
			if err == nil {
				t.Fatal("expected error for invalid latitude")
			}
			if !strings.Contains(err.Error(), "lat") {
				t.Errorf("error should mention lat: %v", err)
			}
		})
	}
}

func TestConfigValidate_InvalidLongitude(t *testing.T) {
	tests := []struct {
		name string
		lon  float64
	}{
		{"lon too low", -181},
		{"lon too high", 181},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig(t)
			cfg.Events.Scheduler.Enabled = boolPtr(true)
			cfg.Events.Scheduler.Geo.Enabled = boolPtr(true)
			cfg.Events.Scheduler.Geo.Lat = 60
			cfg.Events.Scheduler.Geo.Lon = tt.lon

			err := cfg.Validate()
			if err == nil {
				t.Fatal("expected error for invalid longitude")
			}
			if !strings.Contains(err.Error(), "lon") {
				t.Errorf("error should mention lon: %v", err)
			}
		})
	}
}

func TestConfigValidate_InvalidTimezone(t *testing.T) {
	cfg := validConfig(t)
	cfg.Events.Scheduler.Enabled = boolPtr(true)
	cfg.Events.Scheduler.Timezone = "Invalid/Timezone"
	cfg.Events.Scheduler.Geo.Enabled = boolPtr(true)
	cfg.Events.Scheduler.Geo.Location = "Test"

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for invalid timezone")
	}
	if !strings.Contains(err.Error(), "timezone") {
		t.Errorf("error should mention timezone: %v", err)
	}
}

func TestConfigValidate_ValidTimezone(t *testing.T) {
	cfg := validConfig(t)
	cfg.Events.Scheduler.Enabled = boolPtr(true)
	cfg.Events.Scheduler.Timezone = "Europe/Helsinki"
	cfg.Events.Scheduler.Geo.Enabled = boolPtr(true)
	cfg.Events.Scheduler.Geo.Location = "Test"

	err := cfg.Validate()
	if err != nil {
		t.Errorf("valid timezone should be accepted: %v", err)
	}
}

func TestConfigValidate_SSERetryBackoff(t *testing.T) {
	cfg := validConfig(t)
	cfg.Events.SSE.MinRetryBackoff = Duration(10 * time.Second)
	cfg.Events.SSE.MaxRetryBackoff = Duration(5 * time.Second) // min > max

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for min > max backoff")
	}
	if !strings.Contains(err.Error(), "sse") {
		t.Errorf("error should mention sse: %v", err)
	}
}

func TestConfigValidate_NegativeReconcilerValues(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*Config)
		field string
	}{
		{
			name: "negative debounce",
			setup: func(c *Config) {
				c.Reconciler.DebounceMs = -1
			},
			field: "reconciler.debounce_ms",
		},
		{
			name: "negative rate limit",
			setup: func(c *Config) {
				c.Reconciler.RateLimitRPS = -1
			},
			field: "reconciler.rate_limit_rps",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig(t)
			tt.setup(cfg)

			err := cfg.Validate()
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tt.field) {
				t.Errorf("error should mention %s: %v", tt.field, err)
			}
		})
	}
}

func TestConfigValidate_NegativeEventBusValues(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*Config)
		field string
	}{
		{
			name: "negative workers",
			setup: func(c *Config) {
				c.EventBus.Workers = -1
			},
			field: "eventbus.workers",
		},
		{
			name: "negative queue size",
			setup: func(c *Config) {
				c.EventBus.QueueSize = -1
			},
			field: "eventbus.queue_size",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig(t)
			tt.setup(cfg)

			err := cfg.Validate()
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tt.field) {
				t.Errorf("error should mention %s: %v", tt.field, err)
			}
		})
	}
}

func TestConfigValidate_MultipleErrors(t *testing.T) {
	cfg := validConfig(t)
	cfg.Hue.Bridge = ""
	cfg.Hue.Token = ""
	cfg.Log.Level = "invalid"

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected errors")
	}

	errs, ok := err.(ValidationErrors)
	if !ok {
		t.Fatalf("expected ValidationErrors, got %T", err)
	}

	if len(errs) < 3 {
		t.Errorf("expected at least 3 errors, got %d: %v", len(errs), errs)
	}
}

func TestValidationError_Error(t *testing.T) {
	err := ValidationError{
		Field:   "test.field",
		Message: "test message",
	}

	expected := "config.test.field: test message"
	if err.Error() != expected {
		t.Errorf("expected %q, got %q", expected, err.Error())
	}
}

func TestValidationErrors_Error(t *testing.T) {
	errs := ValidationErrors{
		{Field: "a", Message: "error a"},
		{Field: "b", Message: "error b"},
	}

	errStr := errs.Error()
	if !strings.Contains(errStr, "2 configuration errors") {
		t.Errorf("should mention count: %s", errStr)
	}
	if !strings.Contains(errStr, "error a") {
		t.Errorf("should contain error a: %s", errStr)
	}
	if !strings.Contains(errStr, "error b") {
		t.Errorf("should contain error b: %s", errStr)
	}
}

func TestValidationErrors_SingleError(t *testing.T) {
	errs := ValidationErrors{
		{Field: "a", Message: "error a"},
	}

	errStr := errs.Error()
	// Single error should not show count
	if strings.Contains(errStr, "configuration errors") {
		t.Errorf("single error should not show count: %s", errStr)
	}
	if !strings.Contains(errStr, "config.a: error a") {
		t.Errorf("should contain error: %s", errStr)
	}
}

func TestValidationErrors_Empty(t *testing.T) {
	errs := ValidationErrors{}
	if errs.Error() != "no validation errors" {
		t.Errorf("unexpected message: %s", errs.Error())
	}
}

// Test that default script path is validated
func TestConfigValidate_DefaultScriptPath(t *testing.T) {
	cfg := &Config{
		Hue: HueConfig{
			Bridge: "192.168.1.1",
			Token:  "test-token",
		},
		// Script not set - will use default "main.lua"
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for missing default script")
	}

	if !strings.Contains(err.Error(), "script") {
		t.Errorf("error should mention script: %v", err)
	}
	if !strings.Contains(err.Error(), "main.lua") {
		t.Errorf("error should mention main.lua: %v", err)
	}
}

// Test HTTP client validation
func TestConfigValidate_HTTPClient(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*Config)
		field string
	}{
		{
			name: "negative timeout",
			setup: func(c *Config) {
				c.Clients.HTTP.Timeout = Duration(-1 * time.Second)
			},
			field: "clients.http.timeout",
		},
		{
			name: "negative max response size",
			setup: func(c *Config) {
				c.Clients.HTTP.MaxResponseSize = -1
			},
			field: "clients.http.max_response_size",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig(t)
			tt.setup(cfg)

			err := cfg.Validate()
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tt.field) {
				t.Errorf("error should mention %s: %v", tt.field, err)
			}
		})
	}
}
