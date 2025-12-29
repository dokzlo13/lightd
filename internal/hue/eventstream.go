package hue

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/dokzlo13/lightd/internal/eventbus"
)

// ErrMaxReconnectsExceeded is returned when the maximum number of reconnect attempts is exceeded.
var ErrMaxReconnectsExceeded = errors.New("max reconnects exceeded")

// EventStreamConfig contains configuration for event stream reconnection.
type EventStreamConfig struct {
	MinBackoff    time.Duration // Minimum backoff between reconnects
	MaxBackoff    time.Duration // Maximum backoff between reconnects
	Multiplier    float64       // Backoff multiplier
	MaxReconnects int           // Max reconnect attempts, 0 = infinite
}

// DefaultEventStreamConfig returns sensible defaults for event stream configuration.
func DefaultEventStreamConfig() EventStreamConfig {
	return EventStreamConfig{
		MinBackoff:    1 * time.Second,
		MaxBackoff:    2 * time.Minute,
		Multiplier:    2.0,
		MaxReconnects: 0, // infinite
	}
}

// EventStream listens to the Hue event stream (SSE)
type EventStream struct {
	client     *Client
	httpClient *http.Client
	config     EventStreamConfig
}

// NewEventStream creates a new event stream listener
func NewEventStream(client *Client) *EventStream {
	return NewEventStreamWithConfig(client, DefaultEventStreamConfig())
}

// NewEventStreamWithConfig creates a new event stream listener with custom configuration
func NewEventStreamWithConfig(client *Client, config EventStreamConfig) *EventStream {
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}

	return &EventStream{
		client: client,
		httpClient: &http.Client{
			Transport: transport,
			// No timeout for SSE - it's a long-lived connection
		},
		config: config,
	}
}

// Run starts listening to the event stream with automatic reconnection.
// Returns ErrMaxReconnectsExceeded if max reconnects is exceeded.
func (e *EventStream) Run(ctx context.Context, bus *eventbus.Bus) error {
	retryCount := 0
	currentBackoff := e.config.MinBackoff

	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		err := e.connect(ctx, bus)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}

			retryCount++

			// Check if we exceeded max reconnects
			if e.config.MaxReconnects > 0 && retryCount > e.config.MaxReconnects {
				log.Error().
					Int("max_reconnects", e.config.MaxReconnects).
					Msg("Event stream: max reconnects exceeded, terminating")
				return ErrMaxReconnectsExceeded
			}

			log.Warn().
				Err(err).
				Dur("backoff", currentBackoff).
				Int("retry", retryCount).
				Int("max_reconnects", e.config.MaxReconnects).
				Msg("Event stream disconnected, reconnecting")

			select {
			case <-ctx.Done():
				return nil
			case <-time.After(currentBackoff):
			}

			// Calculate next backoff with multiplier, capped at max
			nextBackoff := time.Duration(float64(currentBackoff) * e.config.Multiplier)
			if nextBackoff > e.config.MaxBackoff {
				nextBackoff = e.config.MaxBackoff
			}
			currentBackoff = nextBackoff

			continue
		}

		// Reset retry count and backoff on successful connection
		retryCount = 0
		currentBackoff = e.config.MinBackoff
	}
}

func (e *EventStream) connect(ctx context.Context, bus *eventbus.Bus) error {
	url := fmt.Sprintf("https://%s/eventstream/clip/v2", e.client.Address())

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}

	req.Header.Set("hue-application-key", e.client.token)
	req.Header.Set("Accept", "text/event-stream")

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	log.Info().Msg("Connected to Hue event stream")

	scanner := bufio.NewScanner(resp.Body)
	var dataBuffer strings.Builder

	for scanner.Scan() {
		line := scanner.Text()

		// Handle intro message
		if line == ": hi" {
			log.Debug().Msg("Received event stream greeting")
			continue
		}

		// Empty line marks end of event
		if line == "" {
			if dataBuffer.Len() > 0 {
				e.processEvent(dataBuffer.String(), bus)
				dataBuffer.Reset()
			}
			continue
		}

		// Collect data lines
		if strings.HasPrefix(line, "data: ") {
			dataBuffer.WriteString(strings.TrimPrefix(line, "data: "))
		}
	}

	if err := scanner.Err(); err != nil {
		return err
	}

	return nil
}

func (e *EventStream) processEvent(data string, bus *eventbus.Bus) {
	var events []map[string]interface{}
	if err := json.Unmarshal([]byte(data), &events); err != nil {
		log.Warn().Err(err).Str("data", data).Msg("Failed to parse event")
		return
	}

	for _, event := range events {
		e.handleEvent(event, bus)
	}
}

func (e *EventStream) handleEvent(event map[string]interface{}, bus *eventbus.Bus) {
	eventType, _ := event["type"].(string)
	dataItems, _ := event["data"].([]interface{})

	for _, item := range dataItems {
		itemMap, ok := item.(map[string]interface{})
		if !ok {
			continue
		}

		itemType, _ := itemMap["type"].(string)
		itemID, _ := itemMap["id"].(string)

		switch itemType {
		case "button":
			e.handleButtonEvent(itemID, itemMap, bus)

		case "relative_rotary":
			e.handleRotaryEvent(itemID, itemMap, bus)

		case "zigbee_connectivity":
			e.handleConnectivityEvent(itemID, itemMap, bus)

		default:
			log.Trace().
				Str("event_type", eventType).
				Str("item_type", itemType).
				Str("id", itemID).
				Msg("Unhandled event type")
		}
	}
}

func (e *EventStream) handleButtonEvent(id string, data map[string]interface{}, bus *eventbus.Bus) {
	button, ok := data["button"].(map[string]interface{})
	if !ok {
		return
	}

	buttonReport, ok := button["button_report"].(map[string]interface{})
	if !ok {
		return
	}

	action, _ := buttonReport["event"].(string)
	updated, _ := buttonReport["updated"].(string) // Use updated timestamp as unique event ID

	// Generate a unique event ID from resource ID and timestamp
	eventID := fmt.Sprintf("%s-%s", id, updated)

	log.Debug().
		Str("id", id).
		Str("action", action).
		Str("event_id", eventID).
		Msg("Button event")

	bus.Publish(eventbus.Event{
		Type: eventbus.EventTypeButton,
		Data: map[string]interface{}{
			"resource_id": id,
			"action":      action,
			"event_id":    eventID,
		},
	})
}

func (e *EventStream) handleRotaryEvent(id string, data map[string]interface{}, bus *eventbus.Bus) {
	rotary, ok := data["relative_rotary"].(map[string]interface{})
	if !ok {
		return
	}

	lastEvent, ok := rotary["last_event"].(map[string]interface{})
	if !ok {
		return
	}

	action, _ := lastEvent["action"].(string)
	rotation, _ := lastEvent["rotation"].(map[string]interface{})
	if rotation == nil {
		return
	}

	direction, _ := rotation["direction"].(string)
	steps, _ := rotation["steps"].(float64) // JSON numbers are float64
	duration, _ := rotation["duration"].(float64)

	// Get updated timestamp for event ID
	rotaryReport, _ := rotary["rotary_report"].(map[string]interface{})
	updated, _ := rotaryReport["updated"].(string)
	eventID := fmt.Sprintf("%s-%s", id, updated)

	log.Debug().
		Str("id", id).
		Str("action", action).
		Str("direction", direction).
		Int("steps", int(steps)).
		Str("event_id", eventID).
		Msg("Rotary event")

	bus.Publish(eventbus.Event{
		Type: eventbus.EventTypeRotary,
		Data: map[string]interface{}{
			"resource_id": id,
			"action":      action, // "start" or "repeat"
			"direction":   direction,
			"steps":       int(steps),
			"duration":    int(duration),
			"event_id":    eventID,
		},
	})
}

func (e *EventStream) handleConnectivityEvent(id string, data map[string]interface{}, bus *eventbus.Bus) {
	status, _ := data["status"].(string)

	log.Debug().
		Str("id", id).
		Str("status", status).
		Msg("Connectivity event")

	bus.Publish(eventbus.Event{
		Type: eventbus.EventTypeConnectivity,
		Data: map[string]interface{}{
			"device_id": id,
			"status":    status,
		},
	})
}
