package middleware

import "sync"

// CountCollector flushes after N events
type CountCollector struct {
	mu      sync.Mutex
	events  []map[string]any
	target  int
	onFlush FlushFunc
}

// NewCountCollector creates a new CountCollector
func NewCountCollector(count int, onFlush FlushFunc) *CountCollector {
	return &CountCollector{
		target:  count,
		onFlush: onFlush,
	}
}

// AddEvent adds an event and flushes if target count is reached
func (c *CountCollector) AddEvent(event map[string]any) {
	c.mu.Lock()
	c.events = append(c.events, event)
	shouldFlush := len(c.events) >= c.target
	var events []map[string]any
	if shouldFlush {
		events = c.events
		c.events = nil
	}
	c.mu.Unlock()

	if shouldFlush {
		c.onFlush(events)
	}
}

// Close is a no-op for CountCollector
func (c *CountCollector) Close() {}
