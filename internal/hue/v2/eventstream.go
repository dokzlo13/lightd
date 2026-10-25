package v2

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/dokzlo13/lightd/internal/events"
	"github.com/dokzlo13/lightd/internal/events/sse"
	"github.com/dokzlo13/lightd/internal/hue/bridge"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	stream "github.com/tmaxmax/go-sse"
)

var ErrMaxReconnectsExceeded = errors.New("max reconnects exceeded")

type EventStreamConfig struct {
	MinBackoff, MaxBackoff time.Duration
	Multiplier             float64
	MaxReconnects          int
}
type StreamStatus struct {
	Connected  bool      `json:"connected"`
	Reconnects int       `json:"reconnects"`
	LastEvent  time.Time `json:"last_event,omitempty"`
	LastError  string    `json:"last_error,omitempty"`
}
type EventStream struct {
	v2Client   *Client
	httpClient *http.Client
	config     EventStreamConfig
	mu         sync.RWMutex
	status     StreamStatus
	OnConnect  func(context.Context) error
}

func NewEventStreamWithConfig(c *Client, cfg EventStreamConfig) *EventStream {
	if cfg.MinBackoff <= 0 {
		cfg.MinBackoff = time.Second
	}
	if cfg.MaxBackoff < cfg.MinBackoff {
		cfg.MaxBackoff = time.Minute
	}
	if cfg.Multiplier < 1 {
		cfg.Multiplier = 2
	}
	// Keep bounded dial/TLS/header timeouts from the REST transport, but never
	// impose a total lifetime timeout on the established stream.
	hc := *c.HTTPClient()
	hc.Timeout = 0
	return &EventStream{v2Client: c, httpClient: &hc, config: cfg}
}
func (e *EventStream) Status() StreamStatus { e.mu.RLock(); defer e.mu.RUnlock(); return e.status }
func (e *EventStream) Run(ctx context.Context, bus *events.Bus) error {
	failures := 0
	backoff := e.config.MinBackoff
	for {
		if ctx.Err() != nil {
			return nil
		}
		started := time.Now()
		err := e.connect(ctx, bus)
		if ctx.Err() != nil {
			return nil
		}
		if time.Since(started) >= time.Minute {
			failures = 0
			backoff = e.config.MinBackoff
		}
		failures++
		e.mu.Lock()
		e.status.Connected = false
		e.status.Reconnects++
		if err != nil {
			e.status.LastError = err.Error()
		}
		e.mu.Unlock()
		if e.config.MaxReconnects > 0 && failures > e.config.MaxReconnects {
			return ErrMaxReconnectsExceeded
		}
		log.Warn().Err(err).Dur("backoff", backoff).Msg("Hue event stream reconnecting")
		if bridge.WaitUntil(ctx, backoff) != nil {
			return nil
		}
		backoff = time.Duration(float64(backoff) * e.config.Multiplier)
		if backoff > e.config.MaxBackoff {
			backoff = e.config.MaxBackoff
		}
	}
}
func (e *EventStream) connect(ctx context.Context, bus *events.Bus) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, e.v2Client.BaseURL()+"/eventstream/clip/v2", nil)
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
		return fmt.Errorf("Hue event stream HTTP %d", resp.StatusCode)
	}
	if !strings.HasPrefix(strings.ToLower(resp.Header.Get("Content-Type")), "text/event-stream") {
		return fmt.Errorf("Hue event stream unexpected content type")
	}
	if e.OnConnect != nil {
		if err = e.OnConnect(ctx); err != nil {
			return fmt.Errorf("refresh Hue catalog on reconnect: %w", err)
		}
	}
	e.mu.Lock()
	e.status.Connected = true
	e.status.LastError = ""
	e.mu.Unlock()
	defer func() { e.mu.Lock(); e.status.Connected = false; e.mu.Unlock() }()
	for ev, err := range stream.Read(strictEventReader{resp.Body}, &stream.ReadConfig{MaxEventSize: 1 << 20}) {
		if err != nil {
			return err
		}
		if ev.Data == "" {
			continue
		}
		e.mu.Lock()
		e.status.LastEvent = time.Now().UTC()
		e.mu.Unlock()
		e.processFrame(ev.Data, ev.LastEventID, bus)
	}
	return io.EOF // A clean remote EOF still requires reconnect pacing.
}
func ownerID(data map[string]interface{}) string {
	owner, _ := data["owner"].(map[string]interface{})
	id, _ := owner["rid"].(string)
	return id
}

func (e *EventStream) processEvent(data string, bus *events.Bus) { e.processFrame(data, "", bus) }
func (e *EventStream) processFrame(data, frameID string, bus *events.Bus) {
	var events []map[string]interface{}
	if err := json.Unmarshal([]byte(data), &events); err != nil {
		log.Warn().Err(err).Str("data", data).Msg("Failed to parse event")
		return
	}

	for _, event := range events {
		event["stream_id"] = frameID
		e.handleEvent(event, bus)
	}
}

func (e *EventStream) handleEvent(event map[string]interface{}, bus *events.Bus) {
	eventType, _ := event["type"].(string)
	if eventType != "update" {
		return
	}
	dataItems, _ := event["data"].([]interface{})

	for _, item := range dataItems {
		itemMap, ok := item.(map[string]interface{})
		if !ok {
			continue
		}

		itemMap["container_id"] = event["id"]
		itemMap["stream_id"] = event["stream_id"]
		itemMap["creationtime"] = event["creationtime"]
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
	eventID := eventIdentity(id, updated, data)

	log.Debug().
		Str("id", id).
		Str("action", action).
		Str("event_id", eventID).
		Msg("Button event")

	e.publish(bus, events.Event{
		Type: events.EventTypeButton,
		Data: map[string]interface{}{
			"resource_id":  id,
			"action":       action,
			"event_id":     eventID,
			"container_id": data["container_id"],
			"stream_id":    data["stream_id"],
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
	if updated == "" {
		updated, _ = lastEvent["updated"].(string)
	}
	if updated == "" {
		updated, _ = data["creationtime"].(string)
	}
	eventID := eventIdentity(id, updated, data)

	log.Debug().
		Str("id", id).
		Str("action", action).
		Str("direction", direction).
		Int("steps", int(steps)).
		Str("event_id", eventID).
		Msg("Rotary event")

	e.publish(bus, events.Event{
		Type: events.EventTypeRotary,
		Data: map[string]interface{}{
			"resource_id":  id,
			"action":       action, // "start" or "repeat"
			"direction":    direction,
			"steps":        int(steps),
			"duration":     int(duration),
			"event_id":     eventID,
			"container_id": data["container_id"],
			"stream_id":    data["stream_id"],
		},
	})
}

func (e *EventStream) handleConnectivityEvent(id string, data map[string]interface{}, bus *events.Bus) {
	status, _ := data["status"].(string)
	e.v2Client.UpdateConnectivity(ownerID(data), status)

	log.Debug().
		Str("id", id).
		Str("status", status).
		Msg("Connectivity event")

	e.publish(bus, events.Event{
		Type: events.EventTypeConnectivity,
		Data: map[string]interface{}{
			"device_id":    id,
			"resource_id":  id,
			"event_id":     eventIdentity(id, "", data),
			"container_id": data["container_id"],
			"stream_id":    data["stream_id"],
			"owner_id":     ownerID(data),
			"status":       status,
		},
	})
}

func (e *EventStream) handleLightChangeEvent(id string, data map[string]interface{}, resourceType sse.LightResourceType, bus *events.Bus) {
	eventData := map[string]interface{}{
		"resource_id":   id,
		"resource_type": string(resourceType),
		"event_id":      eventIdentity(id, "", data),
		"container_id":  data["container_id"],
		"stream_id":     data["stream_id"],
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

	e.publish(bus, events.Event{
		Type: events.EventTypeLightChange,
		Data: eventData,
	})
}

// go-sse Read is also a finite-response parser and may dispatch an unfinished
// block at EOF. A persistent Hue stream requires the WHATWG blank terminator.
type strictEventReader struct{ io.Reader }

func (r strictEventReader) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	if err == io.EOF {
		err = io.ErrUnexpectedEOF
	}
	return n, err
}

func (e *EventStream) publish(bus *events.Bus, event events.Event) {
	if bus == nil {
		return
	}
	if err := bus.Publish(event); err != nil {
		log.Warn().Err(err).Str("type", string(event.Type)).Interface("event_id", event.Data["event_id"]).Interface("resource_id", event.Data["resource_id"]).Msg("Hue event rejected by action pipeline")
	}
}

func eventIdentity(id, updated string, data map[string]interface{}) string {
	if updated != "" {
		return id + "-" + updated
	}
	for _, field := range []string{"container_id", "stream_id", "creationtime"} {
		if v, ok := data[field].(string); ok && v != "" {
			return id + "-" + v
		}
	}
	return id + "-" + uuid.NewString()
}
