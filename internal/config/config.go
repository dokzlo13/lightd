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
	Geo             GeoConfig         `yaml:"geo"`
	Database        DatabaseConfig    `yaml:"database"`
	Log             LogConfig         `yaml:"log"`
	Cache           CacheConfig       `yaml:"cache"`
	Reconciler      ReconcilerConfig  `yaml:"reconciler"`
	Ledger          LedgerConfig      `yaml:"ledger"`
	Healthcheck     HealthcheckConfig `yaml:"healthcheck"`
	EventBus        EventBusConfig    `yaml:"eventbus"`
	Script          string            `yaml:"script"`
	ShutdownTimeout Duration          `yaml:"shutdown_timeout"` // General shutdown timeout for graceful stops
}

// HueConfig contains Hue bridge connection settings
type HueConfig struct {
	Bridge  string   `yaml:"bridge"`
	Token   string   `yaml:"token"`
	Timeout Duration `yaml:"timeout"` // HTTP timeout for Hue API requests

	// Event stream reconnect settings
	MinRetryBackoff Duration `yaml:"min_retry_backoff"` // Minimum backoff between reconnects (default: 1s)
	MaxRetryBackoff Duration `yaml:"max_retry_backoff"` // Maximum backoff between reconnects (default: 2m)
	RetryMultiplier float64  `yaml:"retry_multiplier"`  // Backoff multiplier (default: 2.0)
	MaxReconnects   int      `yaml:"max_reconnects"`    // Max reconnect attempts, 0 = infinite (default: 0)
}

// GeoConfig contains geo/location settings for astronomical calculations
type GeoConfig struct {
	Name        string   `yaml:"name"`
	Timezone    string   `yaml:"timezone"`
	Lat         float64  `yaml:"lat,omitempty"`
	Lon         float64  `yaml:"lon,omitempty"`
	HTTPTimeout Duration `yaml:"http_timeout"` // Timeout for geocoding HTTP requests
}

// DatabaseConfig contains database settings
type DatabaseConfig struct {
	Path string `yaml:"path"`
}

// LogConfig contains logging settings
type LogConfig struct {
	Level         string   `yaml:"level"`
	Colors        bool     `yaml:"colors"`
	PrintSchedule Duration `yaml:"print_schedule"` // Interval to print schedule (0 = disabled)
}

// CacheConfig contains cache settings
type CacheConfig struct {
	Enabled         bool     `yaml:"enabled"`          // If false, always fetch fresh state (default: false)
	RefreshInterval Duration `yaml:"refresh_interval"` // Only used if enabled
}

// ReconcilerConfig contains reconciler settings
type ReconcilerConfig struct {
	PeriodicInterval Duration `yaml:"periodic_interval"`
	RateLimitRPS     float64  `yaml:"rate_limit_rps"`
}

// LedgerConfig contains event ledger settings
type LedgerConfig struct {
	CleanupInterval Duration `yaml:"cleanup_interval"`
	RetentionDays   int      `yaml:"retention_days"`
}

// HealthcheckConfig contains health check server settings
type HealthcheckConfig struct {
	Enabled bool   `yaml:"enabled"`
	Host    string `yaml:"host"`
	Port    int    `yaml:"port"`
}

// EventBusConfig contains event bus settings
type EventBusConfig struct {
	Workers   int `yaml:"workers"`    // Number of worker goroutines (default: 4)
	QueueSize int `yaml:"queue_size"` // Event queue size (default: 100)
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

	// Geo defaults
	if cfg.Geo.Timezone == "" {
		cfg.Geo.Timezone = "UTC"
	}
	if cfg.Geo.HTTPTimeout == 0 {
		cfg.Geo.HTTPTimeout = Duration(10 * time.Second)
	}

	// Hue defaults
	if cfg.Hue.Timeout == 0 {
		cfg.Hue.Timeout = Duration(30 * time.Second)
	}
	if cfg.Hue.MinRetryBackoff == 0 {
		cfg.Hue.MinRetryBackoff = Duration(1 * time.Second)
	}
	if cfg.Hue.MaxRetryBackoff == 0 {
		cfg.Hue.MaxRetryBackoff = Duration(2 * time.Minute)
	}
	if cfg.Hue.RetryMultiplier == 0 {
		cfg.Hue.RetryMultiplier = 2.0
	}
	// MaxReconnects defaults to 0 (infinite), no need to set

	// Cache defaults - caching is OFF by default (always fetch fresh state)
	if cfg.Cache.RefreshInterval == 0 {
		cfg.Cache.RefreshInterval = Duration(5 * time.Minute)
	}

	// Reconciler defaults
	if cfg.Reconciler.PeriodicInterval == 0 {
		cfg.Reconciler.PeriodicInterval = Duration(5 * time.Minute)
	}
	if cfg.Reconciler.RateLimitRPS == 0 {
		cfg.Reconciler.RateLimitRPS = 10.0 // 10 requests per second
	}

	// Ledger defaults
	if cfg.Ledger.CleanupInterval == 0 {
		cfg.Ledger.CleanupInterval = Duration(24 * time.Hour)
	}
	if cfg.Ledger.RetentionDays == 0 {
		cfg.Ledger.RetentionDays = 30
	}

	// Healthcheck defaults
	if cfg.Healthcheck.Port == 0 {
		cfg.Healthcheck.Port = 9090
	}
	if cfg.Healthcheck.Host == "" {
		cfg.Healthcheck.Host = "0.0.0.0"
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
