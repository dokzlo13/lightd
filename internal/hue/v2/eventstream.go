package v2

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

	"github.com/dokzlo13/lightd/internal/events"
	"github.com/dokzlo13/lightd/internal/events/sse"
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

// EventStream listens to the Hue event stream (SSE) via V2 API.
// This is the primary reason for this custom V2 client - no Go library
// currently supports Hue V2 SSE events.
type EventStream struct {
	v2Client   *Client
	httpClient *http.Client
	config     EventStreamConfig
}

// NewEventStreamWithConfig creates a new event stream listener with custom configuration
func NewEventStreamWithConfig(v2Client *Client, config EventStreamConfig) *EventStream {
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}

	return &EventStream{
		v2Client: v2Client,
		httpClient: &http.Client{
			Transport: transport,
			// No timeout for SSE - it's a long-lived connection
		},
		config: config,
	}
}

// Run starts listening to the event stream with automatic reconnection.
// Returns ErrMaxReconnectsExceeded if max reconnects is exceeded.
func (e *EventStream) Run(ctx context.Context, bus *events.Bus) error {
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

func (e *EventStream) connect(ctx context.Context, bus *events.Bus) error {
	url := fmt.Sprintf("https://%s/eventstream/clip/v2", e.v2Client.Address())

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}

	req.Header.Set("hue-application-key", e.v2Client.Token())
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

func (e *EventStream) processEvent(data string, bus *events.Bus) {
	var events []map[string]interface{}
	if err := json.Unmarshal([]byte(data), &events); err != nil {
		log.Warn().Err(err).Str("data", data).Msg("Failed to parse event")
		return
	}

	for _, event := range events {
		e.handleEvent(event, bus)
	}
}

func (e *EventStream) handleEvent(event map[string]interface{}, bus *events.Bus) {
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

		case string(sse.LightResourceTypeLight):
			e.handleLightChangeEvent(itemID, itemMap, sse.LightResourceTypeLight, bus)

		case string(sse.LightResourceTypeGroupedLight):
			e.handleLightChangeEvent(itemID, itemMap, sse.LightResourceTypeGroupedLight, bus)

		default:
			log.Trace().
				Str("event_type", eventType).
				Str("item_type", itemType).
				Str("id", itemID).
				Msg("Unhandled event type")
		}
	}
}

func (e *EventStream) handleButtonEvent(id string, data map[string]interface{}, bus *events.Bus) {
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

	bus.Publish(events.Event{
		Type: events.EventTypeButton,
		Data: map[string]interface{}{
			"resource_id": id,
			"action":      action,
			"event_id":    eventID,
		},
	})
}

func (e *EventStream) handleRotaryEvent(id string, data map[string]interface{}, bus *events.Bus) {
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

	bus.Publish(events.Event{
		Type: events.EventTypeRotary,
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

func (e *EventStream) handleConnectivityEvent(id string, data map[string]interface{}, bus *events.Bus) {
	status, _ := data["status"].(string)

	log.Debug().
		Str("id", id).
		Str("status", status).
		Msg("Connectivity event")

	bus.Publish(events.Event{
		Type: events.EventTypeConnectivity,
		Data: map[string]interface{}{
			"device_id": id,
			"status":    status,
		},
	})
}

func (e *EventStream) handleLightChangeEvent(id string, data map[string]interface{}, resourceType sse.LightResourceType, bus *events.Bus) {
	eventData := map[string]interface{}{
		"resource_id":   id,
		"resource_type": string(resourceType),
	}

	// Extract owner info (device or zone/room)
	if owner, ok := data["owner"].(map[string]interface{}); ok {
		if ownerID, ok := owner["rid"].(string); ok {
			eventData["owner_id"] = ownerID
		}
		if ownerType, ok := owner["rtype"].(string); ok {
			eventData["owner_type"] = ownerType
		}
	}

	// Extract dimming info
	if dimming, ok := data["dimming"].(map[string]interface{}); ok {
		if brightness, ok := dimming["brightness"].(float64); ok {
			eventData["brightness"] = brightness
		}
	}

	// Extract on/off state
	if on, ok := data["on"].(map[string]interface{}); ok {
		if isOn, ok := on["on"].(bool); ok {
			eventData["power"] = isOn
		}
	}

	// Extract color temperature
	if colorTemp, ok := data["color_temperature"].(map[string]interface{}); ok {
		if mirek, ok := colorTemp["mirek"].(float64); ok {
			eventData["color_temp_mirek"] = int(mirek)
		}
	}

	// Extract color (xy)
	if color, ok := data["color"].(map[string]interface{}); ok {
		if xy, ok := color["xy"].(map[string]interface{}); ok {
			if x, ok := xy["x"].(float64); ok {
				eventData["color_x"] = x
			}
			if y, ok := xy["y"].(float64); ok {
				eventData["color_y"] = y
			}
		}
	}

	log.Debug().
		Str("id", id).
		Str("resource_type", string(resourceType)).
		Interface("data", eventData).
		Msg("Light change event")

	bus.Publish(events.Event{
		Type: events.EventTypeLightChange,
		Data: eventData,
	})
}
