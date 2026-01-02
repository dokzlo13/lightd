package middleware

// ImmediateCollector flushes on every event (pass-through)
type ImmediateCollector struct {
	onFlush FlushFunc
}

// NewImmediateCollector creates a new ImmediateCollector
func NewImmediateCollector(onFlush FlushFunc) *ImmediateCollector {
	return &ImmediateCollector{onFlush: onFlush}
}

// AddEvent immediately flushes the event
func (c *ImmediateCollector) AddEvent(event map[string]any) {
	c.onFlush([]map[string]any{event})
}

// Close is a no-op for ImmediateCollector
func (c *ImmediateCollector) Close() {}
