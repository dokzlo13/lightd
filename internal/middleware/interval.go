package middleware

import (
	"sync"
	"time"
)

// IntervalCollector flushes every N ms after first event
type IntervalCollector struct {
	mu         sync.Mutex
	events     []map[string]any
	intervalMs int
	timer      *time.Timer
	started    bool
	onFlush    FlushFunc
}

// NewIntervalCollector creates a new IntervalCollector
func NewIntervalCollector(intervalMs int, onFlush FlushFunc) *IntervalCollector {
	return &IntervalCollector{
		intervalMs: intervalMs,
		onFlush:    onFlush,
	}
}

// AddEvent adds an event and starts the interval timer if not already started
func (c *IntervalCollector) AddEvent(event map[string]any) {
	c.mu.Lock()
	c.events = append(c.events, event)

	if !c.started {
		c.timer = time.AfterFunc(time.Duration(c.intervalMs)*time.Millisecond, c.flush)
		c.started = true
	}
	c.mu.Unlock()
}

// flush sends accumulated events to the flush callback
func (c *IntervalCollector) flush() {
	c.mu.Lock()
	events := c.events
	c.events = nil
	c.started = false
	c.mu.Unlock()

	if len(events) > 0 {
		c.onFlush(events)
	}
}

// Close stops the timer
func (c *IntervalCollector) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.timer != nil {
		c.timer.Stop()
	}
}
