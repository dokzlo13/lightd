package v2

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dokzlo13/lightd/internal/events"
	"github.com/dokzlo13/lightd/internal/hue/bridge"
)

func TestSSEParserAcceptsNoSpaceAndMultilineData(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "id:fixture\ndata:[{\ndata:\"type\":\"update\",\"id\":\"container-fixture\",\"data\":[{\"type\":\"button\",\"id\":\"button-fixture\",\"button\":{\"button_report\":{\"event\":\"short_release\",\"updated\":\"2026-01-01T00:00:00Z\"}}}]}]\n\n")
	}))
	defer server.Close()
	c, err := bridge.NewWithHTTP(server.URL, "fixture-token", server.Client(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	es := NewEventStreamWithConfig(c, EventStreamConfig{})
	bus := events.NewBusWithConfig(1, 8)
	defer bus.Close(context.Background())
	received := make(chan events.Event, 1)
	bus.Subscribe(events.EventTypeButton, func(e events.Event) { received <- e })
	_ = es.connect(context.Background(), bus)
	select {
	case e := <-received:
		if e.Data["action"] != "short_release" || e.Data["container_id"] != "container-fixture" {
			t.Fatal(e)
		}
	case <-time.After(time.Second):
		t.Fatal("valid multiline SSE event was lost")
	}
}
func TestSSECleanEOFPacesReconnectAndCancellation(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
	}))
	defer server.Close()
	c, err := bridge.NewWithHTTP(server.URL, "fixture-token", server.Client(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	es := NewEventStreamWithConfig(c, EventStreamConfig{MinBackoff: 80 * time.Millisecond, MaxBackoff: 80 * time.Millisecond, Multiplier: 1})
	ctx, cancel := context.WithTimeout(context.Background(), 210*time.Millisecond)
	defer cancel()
	start := time.Now()
	if err := es.Run(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if n := requests.Load(); n < 2 || n > 3 {
		t.Fatalf("unbounded EOF reconnects: %d", n)
	}
	if time.Since(start) > time.Second {
		t.Fatal("cancellation did not interrupt backoff")
	}
}
func TestSSEDeletionDoesNotProduceButtonAction(t *testing.T) {
	c := bridge.New("fixture.invalid", "fixture-token", time.Second)
	es := NewEventStreamWithConfig(c, EventStreamConfig{})
	bus := events.NewBusWithConfig(1, 8)
	defer bus.Close(context.Background())
	var n atomic.Int32
	bus.Subscribe(events.EventTypeButton, func(events.Event) { n.Add(1) })
	es.processEvent(`[{"type":"delete","data":[{"type":"button","id":"fixture","button":{"button_report":{"event":"short_release","updated":"fixture"}}}]}]`, bus)
	time.Sleep(10 * time.Millisecond)
	if n.Load() != 0 {
		t.Fatal("deletion became a button action")
	}
}
