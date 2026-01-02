package config

import (
	"os"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config represents the application configuration
type Config struct {
	Hue             HueConfig         `yaml:"hue"`
	Database        DatabaseConfig    `yaml:"database"`
	Log             LogConfig         `yaml:"log"`
	Reconciler      ReconcilerConfig  `yaml:"reconciler"`
	Ledger          LedgerConfig      `yaml:"ledger"`
	Healthcheck     HealthcheckConfig `yaml:"healthcheck"`
	Events          EventsConfig      `yaml:"events"`
	EventBus        EventBusConfig    `yaml:"eventbus"`
	KV              KVConfig          `yaml:"kv"`
	Script          string            `yaml:"script"`
	ShutdownTimeout Duration          `yaml:"shutdown_timeout"`
}

// Default top-level values
const (
	DefaultScript          = "main.lua"
	DefaultShutdownTimeout = 5 * time.Second
	DefaultGeoTimezone     = "UTC"
)

// GetScript returns the script path with default
func (c *Config) GetScript() string {
	if c.Script == "" {
		return DefaultScript
	}
	return c.Script
}

// GetShutdownTimeout returns the shutdown timeout with default
func (c *Config) GetShutdownTimeout() time.Duration {
	if c.ShutdownTimeout == 0 {
		return DefaultShutdownTimeout
	}
	return c.ShutdownTimeout.Duration()
}

// EventsConfig groups all event source configurations
type EventsConfig struct {
	Webhook   WebhookConfig   `yaml:"webhook"`
	SSE       SSEConfig       `yaml:"sse"`
	Scheduler SchedulerConfig `yaml:"scheduler"`
}

// HueConfig contains Hue bridge connection settings
type HueConfig struct {
	Bridge  string   `yaml:"bridge"`
	Token   string   `yaml:"token"`
	Timeout Duration `yaml:"timeout"`
}

// Default timeout values
const (
	DefaultHueTimeout         = 30 * time.Second
	DefaultGeoHTTPTimeout     = 10 * time.Second
	DefaultSSEMinRetryBackoff = 1 * time.Second
	DefaultSSEMaxRetryBackoff = 2 * time.Minute
	DefaultSSERetryMultiplier = 2.0
	DefaultSSEMaxReconnects   = 0 // infinite
)

// GetTimeout returns the Hue timeout with default
func (c *HueConfig) GetTimeout() time.Duration {
	if c.Timeout == 0 {
		return DefaultHueTimeout
	}
	return c.Timeout.Duration()
}

// GeoConfig contains geo/location settings for astronomical calculations
type GeoConfig struct {
	Enabled     *bool    `yaml:"enabled"`
	UseCache    *bool    `yaml:"use_cache"`
	Name        string   `yaml:"name"`
	Timezone    string   `yaml:"timezone"`
	Lat         float64  `yaml:"lat,omitempty"`
	Lon         float64  `yaml:"lon,omitempty"`
	HTTPTimeout Duration `yaml:"http_timeout"`
}

// IsEnabled returns whether geo is enabled (defaults to true if not set)
func (c *GeoConfig) IsEnabled() bool {
	if c.Enabled == nil {
		return true
	}
	return *c.Enabled
}

// IsCacheEnabled returns whether geo cache is enabled (defaults to true if not set)
func (c *GeoConfig) IsCacheEnabled() bool {
	if c.UseCache == nil {
		return true
	}
	return *c.UseCache
}

// GetHTTPTimeout returns the HTTP timeout with default
func (c *GeoConfig) GetHTTPTimeout() time.Duration {
	if c.HTTPTimeout == 0 {
		return DefaultGeoHTTPTimeout
	}
	return c.HTTPTimeout.Duration()
}

// GetTimezone returns the timezone with default
func (c *GeoConfig) GetTimezone() string {
	if c.Timezone == "" {
		return DefaultGeoTimezone
	}
	return c.Timezone
}

// DatabaseConfig contains database settings
type DatabaseConfig struct {
	Path string `yaml:"path"`
}

// Default database values
const DefaultDatabasePath = "./hueplanner.sqlite"

// GetPath returns the database path with default
func (c *DatabaseConfig) GetPath() string {
	if c.Path == "" {
		return DefaultDatabasePath
	}
	return c.Path
}

// LogConfig contains logging settings
type LogConfig struct {
	Level   string `yaml:"level"`
	UseJSON bool   `yaml:"use_json"` // If true, use JSON output; if false (default), use text output
	Colors  bool   `yaml:"colors"`   // If true, colorize text output (ignored when use_json is true)
}

// Default log values
const DefaultLogLevel = "info"

// GetLevel returns the log level with default
func (c *LogConfig) GetLevel() string {
	if c.Level == "" {
		return DefaultLogLevel
	}
	return c.Level
}

// ReconcilerConfig contains reconciler settings
type ReconcilerConfig struct {
	Enabled          *bool    `yaml:"enabled"`
	PeriodicInterval Duration `yaml:"periodic_interval"` // 0 = disabled
	DebounceMs       int      `yaml:"debounce_ms"`       // Delay before running reconciliation (0 = immediate)
	RateLimitRPS     float64  `yaml:"rate_limit_rps"`
}

// Default reconciler values
const DefaultReconcilerRateLimitRPS = 10.0

// IsEnabled returns whether the reconciler is enabled (defaults to true if not set)
func (c *ReconcilerConfig) IsEnabled() bool {
	if c.Enabled == nil {
		return true
	}
	return *c.Enabled
}

// GetPeriodicInterval returns the periodic reconciliation interval.
// Returns 0 if disabled (no periodic reconciliation).
func (c *ReconcilerConfig) GetPeriodicInterval() time.Duration {
	return c.PeriodicInterval.Duration()
}

// GetDebounceMs returns the debounce delay in milliseconds.
// Returns 0 for immediate reconciliation (no debounce).
func (c *ReconcilerConfig) GetDebounceMs() int {
	return c.DebounceMs
}

// GetRateLimitRPS returns the rate limit RPS with default
func (c *ReconcilerConfig) GetRateLimitRPS() float64 {
	if c.RateLimitRPS == 0 {
		return DefaultReconcilerRateLimitRPS
	}
	return c.RateLimitRPS
}

// LedgerConfig contains event ledger settings
type LedgerConfig struct {
	Enabled           *bool    `yaml:"enabled"`
	RetentionPeriod   Duration `yaml:"retention_period"`
	RetentionInterval Duration `yaml:"retention_interval"`
}

// Default ledger values
const (
	DefaultLedgerRetentionPeriod   = 30 * 24 * time.Hour // 30 days
	DefaultLedgerRetentionInterval = 24 * time.Hour
)

// IsEnabled returns whether the ledger is enabled (defaults to true if not set)
func (c *LedgerConfig) IsEnabled() bool {
	if c.Enabled == nil {
		return true
	}
	return *c.Enabled
}

// GetRetentionPeriod returns the retention period with default
func (c *LedgerConfig) GetRetentionPeriod() time.Duration {
	if c.RetentionPeriod == 0 {
		return DefaultLedgerRetentionPeriod
	}
	return c.RetentionPeriod.Duration()
}

// GetRetentionInterval returns the retention cleanup interval with default
func (c *LedgerConfig) GetRetentionInterval() time.Duration {
	if c.RetentionInterval == 0 {
		return DefaultLedgerRetentionInterval
	}
	return c.RetentionInterval.Duration()
}

// HealthcheckConfig contains health check server settings
type HealthcheckConfig struct {
	Enabled bool   `yaml:"enabled"`
	Host    string `yaml:"host"`
	Port    int    `yaml:"port"`
}

// Default healthcheck values
const (
	DefaultHealthcheckHost = "0.0.0.0"
	DefaultHealthcheckPort = 9090
)

// GetHost returns the host with default
func (c *HealthcheckConfig) GetHost() string {
	if c.Host == "" {
		return DefaultHealthcheckHost
	}
	return c.Host
}

// GetPort returns the port with default
func (c *HealthcheckConfig) GetPort() int {
	if c.Port == 0 {
		return DefaultHealthcheckPort
	}
	return c.Port
}

// WebhookConfig contains webhook server settings
type WebhookConfig struct {
	Enabled bool   `yaml:"enabled"`
	Host    string `yaml:"host"`
	Port    int    `yaml:"port"`
}

// Default webhook values
const (
	DefaultWebhookHost = "0.0.0.0"
	DefaultWebhookPort = 8081
)

// GetHost returns the host with default
func (c *WebhookConfig) GetHost() string {
	if c.Host == "" {
		return DefaultWebhookHost
	}
	return c.Host
}

// GetPort returns the port with default
func (c *WebhookConfig) GetPort() int {
	if c.Port == 0 {
		return DefaultWebhookPort
	}
	return c.Port
}

// SSEConfig contains SSE (Hue event stream) settings
type SSEConfig struct {
	Enabled         *bool    `yaml:"enabled"`
	MinRetryBackoff Duration `yaml:"min_retry_backoff"`
	MaxRetryBackoff Duration `yaml:"max_retry_backoff"`
	RetryMultiplier float64  `yaml:"retry_multiplier"`
	MaxReconnects   int      `yaml:"max_reconnects"`
}

// IsEnabled returns whether SSE is enabled (defaults to true if not set)
func (c *SSEConfig) IsEnabled() bool {
	if c.Enabled == nil {
		return true
	}
	return *c.Enabled
}

// GetMinRetryBackoff returns the minimum retry backoff with default
func (c *SSEConfig) GetMinRetryBackoff() time.Duration {
	if c.MinRetryBackoff == 0 {
		return DefaultSSEMinRetryBackoff
	}
	return c.MinRetryBackoff.Duration()
}

// GetMaxRetryBackoff returns the maximum retry backoff with default
func (c *SSEConfig) GetMaxRetryBackoff() time.Duration {
	if c.MaxRetryBackoff == 0 {
		return DefaultSSEMaxRetryBackoff
	}
	return c.MaxRetryBackoff.Duration()
}

// GetRetryMultiplier returns the retry multiplier with default
func (c *SSEConfig) GetRetryMultiplier() float64 {
	if c.RetryMultiplier == 0 {
		return DefaultSSERetryMultiplier
	}
	return c.RetryMultiplier
}

// GetMaxReconnects returns the max reconnects (0 = infinite)
func (c *SSEConfig) GetMaxReconnects() int {
	return c.MaxReconnects
}

// SchedulerConfig contains scheduler settings
type SchedulerConfig struct {
	Enabled *bool     `yaml:"enabled"`
	Geo     GeoConfig `yaml:"geo"`
}

// IsEnabled returns whether the scheduler is enabled (defaults to true if not set)
func (c *SchedulerConfig) IsEnabled() bool {
	if c.Enabled == nil {
		return true
	}
	return *c.Enabled
}

// EventBusConfig contains event bus settings
type EventBusConfig struct {
	Workers   int `yaml:"workers"`
	QueueSize int `yaml:"queue_size"`
}

// Default event bus values
const (
	DefaultEventBusWorkers   = 4
	DefaultEventBusQueueSize = 100
)

// GetWorkers returns worker count with default
func (c *EventBusConfig) GetWorkers() int {
	if c.Workers <= 0 {
		return DefaultEventBusWorkers
	}
	return c.Workers
}

// GetQueueSize returns queue size with default
func (c *EventBusConfig) GetQueueSize() int {
	if c.QueueSize <= 0 {
		return DefaultEventBusQueueSize
	}
	return c.QueueSize
}

// KVConfig contains KV store settings
type KVConfig struct {
	CleanupInterval Duration `yaml:"cleanup_interval"`
}

// Default KV values
const DefaultKVCleanupInterval = 5 * time.Minute

// GetCleanupInterval returns the cleanup interval with default
func (c *KVConfig) GetCleanupInterval() time.Duration {
	if c.CleanupInterval == 0 {
		return DefaultKVCleanupInterval
	}
	return c.CleanupInterval.Duration()
}

// Duration is a wrapper around time.Duration for YAML unmarshalling
type Duration time.Duration

// UnmarshalYAML implements yaml.Unmarshaler for Duration
func (d *Duration) UnmarshalYAML(value *yaml.Node) error {
	var s string
	if err := value.Decode(&s); err != nil {
		return err
	}
	parsed, err := time.ParseDuration(s)
	if err != nil {
		return err
	}
	*d = Duration(parsed)
	return nil
}

// Duration returns the underlying time.Duration
func (d Duration) Duration() time.Duration {
	return time.Duration(d)
}

// Load reads and parses the configuration file
// Note: Defaults are handled by accessor methods (Get* functions), not here.
// This keeps defaults centralized in one place per config type.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	// Expand environment variables
	expanded := expandEnvVars(string(data))

	var cfg Config
	if err := yaml.Unmarshal([]byte(expanded), &cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// expandEnvVars expands environment variables in the format ${VAR} or ${VAR:default}
func expandEnvVars(input string) string {
	// Match ${VAR} or ${VAR:default}
	re := regexp.MustCompile(`\$\{([^}:]+)(?::([^}]*))?\}`)

	return re.ReplaceAllStringFunc(input, func(match string) string {
		parts := re.FindStringSubmatch(match)
		if len(parts) < 2 {
			return match
		}

		varName := parts[1]
		defaultVal := ""
		if len(parts) >= 3 {
			defaultVal = parts[2]
		}

		if val := os.Getenv(varName); val != "" {
			return val
		}
		return defaultVal
	})
}

// ExpandEnvString expands a single string with environment variables
func ExpandEnvString(s string) string {
	if strings.HasPrefix(s, "${") && strings.HasSuffix(s, "}") {
		return expandEnvVars(s)
	}
	return s
}
