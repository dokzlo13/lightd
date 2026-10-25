package events

import (
	"context"
	"errors"
	"github.com/rs/zerolog/log"
	"sync"
	"sync/atomic"
)

type EventType string

const (
	EventTypeButton       EventType = "button"
	EventTypeRotary       EventType = "rotary"
	EventTypeConnectivity EventType = "connectivity"
	EventTypeLightChange  EventType = "light_change"
	EventTypeSchedule     EventType = "schedule"
	EventTypeWebhook      EventType = "webhook"
)
const (
	DefaultWorkerCount = 4
	DefaultQueueSize   = 100
)

var ErrClosed = errors.New("event bus closed")
var ErrQueueFull = errors.New("event bus queue full")

type Event struct {
	Type EventType
	Data map[string]interface{}
}
type Handler func(Event)
type work struct {
	event    Event
	handlers []Handler
}

// Ack handlers must only admit bounded work, never execute Lua. They run in
// publication order. Legacy handlers share one worker to preserve input order.
type Bus struct {
	mu        sync.Mutex
	publishMu sync.Mutex
	handlers  map[EventType][]Handler
	ack       map[EventType][]func(Event) error
	workQueue chan work
	closed    bool
	done      chan struct{}
	rejected  atomic.Uint64
}

func NewBus() *Bus { return NewBusWithConfig(DefaultWorkerCount, DefaultQueueSize) }
func NewBusWithConfig(workers, size int) *Bus {
	if size < 1 {
		size = DefaultQueueSize
	}
	b := &Bus{handlers: map[EventType][]Handler{}, ack: map[EventType][]func(Event) error{}, workQueue: make(chan work, size), done: make(chan struct{})}
	go b.worker()
	return b
}
func (b *Bus) worker() {
	defer close(b.done)
	for w := range b.workQueue {
		for _, h := range w.handlers {
			func() {
				defer func() {
					if p := recover(); p != nil {
						log.Error().Interface("panic", p).Msg("Event handler panicked")
					}
				}()
				h(w.event)
			}()
		}
	}
}
func (b *Bus) Subscribe(t EventType, h Handler) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.handlers[t] = append(b.handlers[t], h)
}
func (b *Bus) SubscribeAck(t EventType, h func(Event) error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.ack[t] = append(b.ack[t], h)
}
func (b *Bus) Publish(e Event) error {
	b.publishMu.Lock()
	defer b.publishMu.Unlock()
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		b.rejected.Add(1)
		return ErrClosed
	}
	hs := append([]Handler(nil), b.handlers[e.Type]...)
	as := append([]func(Event) error(nil), b.ack[e.Type]...)
	b.mu.Unlock()
	for _, h := range as {
		if err := h(e); err != nil {
			b.rejected.Add(1)
			return err
		}
	}
	if len(hs) > 0 {
		select {
		case b.workQueue <- work{e, hs}:
		default:
			b.rejected.Add(1)
			return ErrQueueFull
		}
	}
	return nil
}
func (b *Bus) Close(ctx context.Context) {
	b.publishMu.Lock()
	b.mu.Lock()
	if !b.closed {
		b.closed = true
		close(b.workQueue)
	}
	b.mu.Unlock()
	b.publishMu.Unlock()
	select {
	case <-b.done:
	case <-ctx.Done():
	}
}
func (b *Bus) Clear() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.handlers = map[EventType][]Handler{}
	b.ack = map[EventType][]func(Event) error{}
}
func (b *Bus) Rejected() uint64 { return b.rejected.Load() }
