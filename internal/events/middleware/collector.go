package middleware

// FlushFunc is called when collector flushes events
type FlushFunc func(events []map[string]any)

// Collector accumulates events and flushes based on strategy
type Collector interface {
	AddEvent(event map[string]any)
	Close()
}
