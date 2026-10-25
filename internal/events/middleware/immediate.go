package middleware

import "sync"

type ImmediateCollector struct {
	mu      sync.Mutex
	closed  bool
	onFlush FlushFunc
}

func NewImmediateCollector(fn FlushFunc) *ImmediateCollector { return &ImmediateCollector{onFlush: fn} }
func (c *ImmediateCollector) AddEvent(e map[string]any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return ErrCollectorClosed
	}
	return c.onFlush([]map[string]any{e})
}
func (c *ImmediateCollector) Close() { c.mu.Lock(); defer c.mu.Unlock(); c.closed = true }
