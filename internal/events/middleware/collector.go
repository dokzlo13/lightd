package middleware

import (
	"errors"
	"github.com/rs/zerolog/log"
	"sync"
	"time"
)

var ErrCollectorClosed = errors.New("collector closed")
var ErrCollectorFull = errors.New("collector capacity exceeded")

const MaxCollectedEvents = 256

// FlushFunc returns only after downstream queue admission succeeds or rejects.
type FlushFunc func([]map[string]any) error
type Collector interface {
	AddEvent(map[string]any) error
	Close()
}

// bufferedCollector owns its timer and admission as one serialized operation.
// Timer generations prevent an expired quiet timer flushing a newer window.
type bufferedCollector struct {
	mu         sync.Mutex
	events     []map[string]any
	timer      *time.Timer
	generation uint64
	closed     bool
	mode       string
	limit      int
	delay      time.Duration
	first      time.Time
	onFlush    FlushFunc
}

func newBuffered(mode string, n int, fn FlushFunc) *bufferedCollector {
	if n < 1 {
		n = 1
	}
	return &bufferedCollector{mode: mode, limit: n, delay: time.Duration(n) * time.Millisecond, onFlush: fn}
}
func (c *bufferedCollector) schedule(d time.Duration) {
	if c.timer != nil {
		c.timer.Stop()
	}
	c.generation++
	g := c.generation
	c.timer = time.AfterFunc(d, func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		if c.closed || g != c.generation {
			return
		}
		c.flush()
	})
}
func (c *bufferedCollector) flush() error {
	if len(c.events) == 0 {
		return nil
	}
	if err := c.onFlush(c.events); err != nil {
		log.Warn().Err(err).Int("events", len(c.events)).Msg("Collector admission rejected; retaining batch")
		c.schedule(100 * time.Millisecond)
		return err
	}
	c.events = nil
	c.first = time.Time{}
	return nil
}
func (c *bufferedCollector) AddEvent(e map[string]any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return ErrCollectorClosed
	}
	if len(c.events) >= MaxCollectedEvents {
		return ErrCollectorFull
	}
	if len(c.events) == 0 {
		c.first = time.Now()
	}
	c.events = append(c.events, e)
	if c.mode == "count" && len(c.events) >= c.limit {
		return c.flush()
	}
	delay := c.delay
	if c.mode == "quiet" {
		if left := 2*time.Second - time.Since(c.first); left < delay {
			delay = left
		}
		if delay < 0 {
			delay = 0
		}
		c.schedule(delay)
	} else if len(c.events) == 1 {
		if c.mode == "count" {
			delay = 2 * time.Second
		}
		c.schedule(delay)
	}
	return nil
}
func (c *bufferedCollector) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	c.closed = true
	c.generation++
	if c.timer != nil {
		c.timer.Stop()
	}
	if len(c.events) > 0 {
		log.Warn().Int("events", len(c.events)).Msg("Collector closed with pending events; batch cancelled")
	}
	c.events = nil
}
