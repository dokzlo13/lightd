package middleware

import (
	"sync"
	"time"
)

// QuietCollector flushes after a quiet period (no new events for N ms)
type QuietCollector struct {
	mu      sync.Mutex
	events  []map[string]any
	timer   *time.Timer
	quietMs int
	onFlush FlushFunc
}

// NewQuietCollector creates a new QuietCollector
func NewQuietCollector(quietMs int, onFlush FlushFunc) *QuietCollector {
	return &QuietCollector{
		quietMs: quietMs,
		onFlush: onFlush,
	}
}

// AddEvent adds an event and resets the quiet timer
func (c *QuietCollector) AddEvent(event map[string]any) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.events = append(c.events, event)

	if c.timer != nil {
		c.timer.Stop()
	}
	c.timer = time.AfterFunc(time.Duration(c.quietMs)*time.Millisecond, c.flush)
}

// flush sends accumulated events to the flush callback
func (c *QuietCollector) flush() {
	c.mu.Lock()
	events := c.events
	c.events = nil
	c.mu.Unlock()

	if len(events) > 0 {
		c.onFlush(events)
	}
}

// Close stops the timer
func (c *QuietCollector) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.timer != nil {
		c.timer.Stop()
	}
}
