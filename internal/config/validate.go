package config

import (
	"errors"
	"fmt"
	"os"
	"time"
)

// ValidationError represents a configuration validation error
type ValidationError struct {
	Field   string
	Message string
}

func (e ValidationError) Error() string {
	return fmt.Sprintf("config.%s: %s", e.Field, e.Message)
}

// ValidationErrors is a collection of validation errors
type ValidationErrors []ValidationError

func (ve ValidationErrors) Error() string {
	if len(ve) == 0 {
		return "no validation errors"
	}
	if len(ve) == 1 {
		return ve[0].Error()
	}

	msg := fmt.Sprintf("%d configuration errors:\n", len(ve))
	for i, e := range ve {
		msg += fmt.Sprintf("  %d. %s\n", i+1, e.Error())
	}
	return msg
}

// Validate performs comprehensive validation of the configuration.
// Returns nil if valid, or ValidationErrors with all detected issues.
// This is designed for fail-fast behavior at startup.
func (c *Config) Validate() error {
	var errs ValidationErrors

	// Hue configuration
	errs = append(errs, c.Hue.Validate()...)

	// Script validation
	errs = append(errs, c.validateScript()...)

	// Database validation
	errs = append(errs, c.Database.Validate()...)

	// Log validation
	errs = append(errs, c.Log.Validate()...)

	// Reconciler validation
	errs = append(errs, c.Reconciler.Validate()...)

	// Ledger validation
	errs = append(errs, c.Ledger.Validate()...)

	// Healthcheck validation
	errs = append(errs, c.Healthcheck.Validate()...)

	// Events validation
	errs = append(errs, c.Events.Validate()...)

	// EventBus validation
	errs = append(errs, c.EventBus.Validate()...)

	// Clients validation
	errs = append(errs, c.Clients.Validate()...)

	if len(errs) > 0 {
		return errs
	}
	return nil
}

// validateScript checks that the script file exists
func (c *Config) validateScript() (errs []ValidationError) {
	scriptPath := c.GetScript()
	if _, err := os.Stat(scriptPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			errs = append(errs, ValidationError{
				Field:   "script",
				Message: fmt.Sprintf("script file not found: %s", scriptPath),
			})
		} else {
			errs = append(errs, ValidationError{
				Field:   "script",
				Message: fmt.Sprintf("cannot access script file %s: %v", scriptPath, err),
			})
		}
	}
	return errs
}

// Validate validates HueConfig
func (c *HueConfig) Validate() (errs []ValidationError) {
	if c.Bridge == "" {
		errs = append(errs, ValidationError{
			Field:   "hue.bridge",
			Message: "bridge address is required",
		})
	}
	if c.Token == "" {
		errs = append(errs, ValidationError{
			Field:   "hue.token",
			Message: "bridge token is required",
		})
	}
	if c.Timeout != 0 && c.Timeout.Duration() < 0 {
		errs = append(errs, ValidationError{
			Field:   "hue.timeout",
			Message: "timeout must be positive",
		})
	}
	return errs
}

// Validate validates DatabaseConfig
func (c *DatabaseConfig) Validate() (errs []ValidationError) {
	// Database path is optional (has default), but if specified should be accessible
	// We don't validate here as the directory might not exist yet and will be created
	return nil
}

// Validate validates LogConfig
func (c *LogConfig) Validate() (errs []ValidationError) {
	validLevels := map[string]bool{
		"":      true, // Will use default
		"trace": true,
		"debug": true,
		"info":  true,
		"warn":  true,
		"error": true,
		"fatal": true,
		"panic": true,
	}

	if !validLevels[c.Level] {
		errs = append(errs, ValidationError{
			Field:   "log.level",
			Message: fmt.Sprintf("invalid log level %q (valid: trace, debug, info, warn, error, fatal, panic)", c.Level),
		})
	}
	return errs
}

// Validate validates ReconcilerConfig
func (c *ReconcilerConfig) Validate() (errs []ValidationError) {
	if c.PeriodicInterval != 0 && c.PeriodicInterval.Duration() < 0 {
		errs = append(errs, ValidationError{
			Field:   "reconciler.periodic_interval",
			Message: "periodic interval must be positive",
		})
	}
	if c.DebounceMs < 0 {
		errs = append(errs, ValidationError{
			Field:   "reconciler.debounce_ms",
			Message: "debounce must be non-negative",
		})
	}
	if c.RateLimitRPS < 0 {
		errs = append(errs, ValidationError{
			Field:   "reconciler.rate_limit_rps",
			Message: "rate limit must be non-negative",
		})
	}
	return errs
}

// Validate validates LedgerConfig
func (c *LedgerConfig) Validate() (errs []ValidationError) {
	if c.RetentionPeriod != 0 && c.RetentionPeriod.Duration() < 0 {
		errs = append(errs, ValidationError{
			Field:   "ledger.retention_period",
			Message: "retention period must be positive",
		})
	}
	if c.RetentionInterval != 0 && c.RetentionInterval.Duration() < 0 {
		errs = append(errs, ValidationError{
			Field:   "ledger.retention_interval",
			Message: "retention interval must be positive",
		})
	}
	return errs
}

// Validate validates HealthcheckConfig
func (c *HealthcheckConfig) Validate() (errs []ValidationError) {
	if c.Enabled {
		if c.Port != 0 && (c.Port < 1 || c.Port > 65535) {
			errs = append(errs, ValidationError{
				Field:   "healthcheck.port",
				Message: fmt.Sprintf("port must be between 1 and 65535, got %d", c.Port),
			})
		}
	}
	return errs
}

// Validate validates EventsConfig
func (c *EventsConfig) Validate() (errs []ValidationError) {
	errs = append(errs, c.Webhook.Validate()...)
	errs = append(errs, c.SSE.Validate()...)
	errs = append(errs, c.Scheduler.Validate()...)
	return errs
}

// Validate validates WebhookConfig
func (c *WebhookConfig) Validate() (errs []ValidationError) {
	if c.Enabled {
		if c.Port != 0 && (c.Port < 1 || c.Port > 65535) {
			errs = append(errs, ValidationError{
				Field:   "events.webhook.port",
				Message: fmt.Sprintf("port must be between 1 and 65535, got %d", c.Port),
			})
		}
	}
	return errs
}

// Validate validates SSEConfig
func (c *SSEConfig) Validate() (errs []ValidationError) {
	if c.MinRetryBackoff != 0 && c.MinRetryBackoff.Duration() < 0 {
		errs = append(errs, ValidationError{
			Field:   "events.sse.min_retry_backoff",
			Message: "min retry backoff must be positive",
		})
	}
	if c.MaxRetryBackoff != 0 && c.MaxRetryBackoff.Duration() < 0 {
		errs = append(errs, ValidationError{
			Field:   "events.sse.max_retry_backoff",
			Message: "max retry backoff must be positive",
		})
	}
	if c.MinRetryBackoff != 0 && c.MaxRetryBackoff != 0 &&
		c.MinRetryBackoff.Duration() > c.MaxRetryBackoff.Duration() {
		errs = append(errs, ValidationError{
			Field:   "events.sse",
			Message: "min_retry_backoff cannot be greater than max_retry_backoff",
		})
	}
	if c.RetryMultiplier < 0 {
		errs = append(errs, ValidationError{
			Field:   "events.sse.retry_multiplier",
			Message: "retry multiplier must be non-negative",
		})
	}
	if c.MaxReconnects < 0 {
		errs = append(errs, ValidationError{
			Field:   "events.sse.max_reconnects",
			Message: "max reconnects must be non-negative (0 = unlimited)",
		})
	}
	return errs
}

// Validate validates SchedulerConfig
func (c *SchedulerConfig) Validate() (errs []ValidationError) {
	if !c.IsEnabled() {
		return nil // Skip validation if scheduler is disabled
	}

	// Validate timezone (required for scheduler, used for all time expressions)
	if c.Timezone != "" {
		if _, err := time.LoadLocation(c.Timezone); err != nil {
			errs = append(errs, ValidationError{
				Field:   "events.scheduler.timezone",
				Message: fmt.Sprintf("invalid timezone %q: %v", c.Timezone, err),
			})
		}
	}

	errs = append(errs, c.Geo.Validate(c.IsEnabled())...)
	return errs
}

// Validate validates GeoConfig
// schedulerEnabled indicates whether the parent scheduler is enabled
func (c *GeoConfig) Validate(schedulerEnabled bool) (errs []ValidationError) {
	if !schedulerEnabled || !c.IsEnabled() {
		return nil // Skip validation if disabled
	}

	// If geo is enabled but no location is configured, fail fast
	hasCoordinates := c.Lat != 0 || c.Lon != 0
	hasLocationName := c.Location != ""

	if !hasCoordinates && !hasLocationName {
		errs = append(errs, ValidationError{
			Field:   "events.scheduler.geo",
			Message: "geo is enabled but no location configured: set either lat/lon or location",
		})
	}

	// Validate lat/lon ranges
	if hasCoordinates {
		if c.Lat < -90 || c.Lat > 90 {
			errs = append(errs, ValidationError{
				Field:   "events.scheduler.geo.lat",
				Message: fmt.Sprintf("latitude must be between -90 and 90, got %f", c.Lat),
			})
		}
		if c.Lon < -180 || c.Lon > 180 {
			errs = append(errs, ValidationError{
				Field:   "events.scheduler.geo.lon",
				Message: fmt.Sprintf("longitude must be between -180 and 180, got %f", c.Lon),
			})
		}
	}

	if c.HTTPTimeout != 0 && c.HTTPTimeout.Duration() < 0 {
		errs = append(errs, ValidationError{
			Field:   "events.scheduler.geo.http_timeout",
			Message: "HTTP timeout must be positive",
		})
	}

	return errs
}

// Validate validates EventBusConfig
func (c *EventBusConfig) Validate() (errs []ValidationError) {
	// Negative values are handled by GetWorkers/GetQueueSize with defaults
	// But we should warn about invalid explicit values
	if c.Workers < 0 {
		errs = append(errs, ValidationError{
			Field:   "eventbus.workers",
			Message: "workers must be non-negative",
		})
	}
	if c.QueueSize < 0 {
		errs = append(errs, ValidationError{
			Field:   "eventbus.queue_size",
			Message: "queue size must be non-negative",
		})
	}
	return errs
}

// Validate validates ClientsConfig
func (c *ClientsConfig) Validate() (errs []ValidationError) {
	errs = append(errs, c.HTTP.Validate()...)
	return errs
}

// Validate validates HTTPClientConfig
func (c *HTTPClientConfig) Validate() (errs []ValidationError) {
	if c.Timeout != 0 && c.Timeout.Duration() < 0 {
		errs = append(errs, ValidationError{
			Field:   "clients.http.timeout",
			Message: "timeout must be positive",
		})
	}
	if c.MaxResponseSize < 0 {
		errs = append(errs, ValidationError{
			Field:   "clients.http.max_response_size",
			Message: "max response size must be non-negative",
		})
	}
	return errs
}
