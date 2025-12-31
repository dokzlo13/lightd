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
	Script          string            `yaml:"script"`
	ShutdownTimeout Duration          `yaml:"shutdown_timeout"`
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

// DatabaseConfig contains database settings
type DatabaseConfig struct {
	Path string `yaml:"path"`
}

// LogConfig contains logging settings
type LogConfig struct {
	Level  string `yaml:"level"`
	Colors bool   `yaml:"colors"`
}

// ReconcilerConfig contains reconciler settings
type ReconcilerConfig struct {
	Enabled          *bool    `yaml:"enabled"`
	PeriodicInterval Duration `yaml:"periodic_interval"`
	RateLimitRPS     float64  `yaml:"rate_limit_rps"`
}

// IsEnabled returns whether the reconciler is enabled (defaults to true if not set)
func (c *ReconcilerConfig) IsEnabled() bool {
	if c.Enabled == nil {
		return true
	}
	return *c.Enabled
}

// LedgerConfig contains event ledger settings
type LedgerConfig struct {
	Enabled           *bool    `yaml:"enabled"`
	RetentionPeriod   Duration `yaml:"retention_period"`
	RetentionInterval Duration `yaml:"retention_interval"`
}

// IsEnabled returns whether the ledger is enabled (defaults to true if not set)
func (c *LedgerConfig) IsEnabled() bool {
	if c.Enabled == nil {
		return true
	}
	return *c.Enabled
}

// HealthcheckConfig contains health check server settings
type HealthcheckConfig struct {
	Enabled bool   `yaml:"enabled"`
	Host    string `yaml:"host"`
	Port    int    `yaml:"port"`
}

// WebhookConfig contains webhook server settings
type WebhookConfig struct {
	Enabled bool   `yaml:"enabled"`
	Host    string `yaml:"host"`
	Port    int    `yaml:"port"`
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

// GetWorkers returns worker count with default
func (c *EventBusConfig) GetWorkers() int {
	if c.Workers <= 0 {
		return 4
	}
	return c.Workers
}

// GetQueueSize returns queue size with default
func (c *EventBusConfig) GetQueueSize() int {
	if c.QueueSize <= 0 {
		return 100
	}
	return c.QueueSize
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

	// Set defaults
	if cfg.Log.Level == "" {
		cfg.Log.Level = "info"
	}
	if cfg.Database.Path == "" {
		cfg.Database.Path = "./hueplanner.sqlite"
	}
	if cfg.Script == "" {
		cfg.Script = "main.lua"
	}

	// Geo defaults (under events.scheduler.geo)
	if cfg.Events.Scheduler.Geo.Timezone == "" {
		cfg.Events.Scheduler.Geo.Timezone = "UTC"
	}
	if cfg.Events.Scheduler.Geo.HTTPTimeout == 0 {
		cfg.Events.Scheduler.Geo.HTTPTimeout = Duration(10 * time.Second)
	}

	// Hue defaults
	if cfg.Hue.Timeout == 0 {
		cfg.Hue.Timeout = Duration(30 * time.Second)
	}

	// SSE reconnect defaults (now under events.sse)
	if cfg.Events.SSE.MinRetryBackoff == 0 {
		cfg.Events.SSE.MinRetryBackoff = Duration(1 * time.Second)
	}
	if cfg.Events.SSE.MaxRetryBackoff == 0 {
		cfg.Events.SSE.MaxRetryBackoff = Duration(2 * time.Minute)
	}
	if cfg.Events.SSE.RetryMultiplier == 0 {
		cfg.Events.SSE.RetryMultiplier = 2.0
	}
	// MaxReconnects defaults to 0 (infinite), no need to set

	// Reconciler defaults
	if cfg.Reconciler.PeriodicInterval == 0 {
		cfg.Reconciler.PeriodicInterval = Duration(5 * time.Minute)
	}
	if cfg.Reconciler.RateLimitRPS == 0 {
		cfg.Reconciler.RateLimitRPS = 10.0
	}

	// Ledger defaults
	if cfg.Ledger.RetentionInterval == 0 {
		cfg.Ledger.RetentionInterval = Duration(24 * time.Hour)
	}
	if cfg.Ledger.RetentionPeriod == 0 {
		cfg.Ledger.RetentionPeriod = Duration(30 * 24 * time.Hour) // 30 days
	}

	// Healthcheck defaults
	if cfg.Healthcheck.Port == 0 {
		cfg.Healthcheck.Port = 9090
	}
	if cfg.Healthcheck.Host == "" {
		cfg.Healthcheck.Host = "0.0.0.0"
	}

	// Webhook defaults
	if cfg.Events.Webhook.Port == 0 {
		cfg.Events.Webhook.Port = 8081
	}
	if cfg.Events.Webhook.Host == "" {
		cfg.Events.Webhook.Host = "0.0.0.0"
	}

	// General shutdown timeout
	if cfg.ShutdownTimeout == 0 {
		cfg.ShutdownTimeout = Duration(5 * time.Second)
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
