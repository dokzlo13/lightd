package eventbus

import (
	"context"
	"sync"

	"github.com/rs/zerolog/log"
)

// EventType represents the type of event
type EventType string

const (
	EventTypeButton       EventType = "button"
	EventTypeRotary       EventType = "rotary"
	EventTypeConnectivity EventType = "connectivity"
	EventTypeSchedule     EventType = "schedule"
	EventTypeWebhook      EventType = "webhook"
)

// Default configuration
const (
	DefaultWorkerCount = 4
	DefaultQueueSize   = 100
)

// Event represents an event in the system
type Event struct {
	Type EventType
	Data map[string]interface{}
}

// Handler is a function that handles events
type Handler func(Event)

// work represents a unit of work for the worker pool
type work struct {
	event   Event
	handler Handler
}

// Bus provides event routing with a bounded worker pool
type Bus struct {
	mu       sync.RWMutex
	handlers map[EventType][]Handler

	// Worker pool
	workQueue chan work
	wg        sync.WaitGroup

	// Shutdown signaling - closing this channel signals publishers to stop
	// Using a channel in select is race-free (unlike mutex + bool)
	closing   chan struct{}
	closeOnce sync.Once
}

// New creates a new event bus with default settings
func New() *Bus {
	return NewWithConfig(DefaultWorkerCount, DefaultQueueSize)
}

// NewWithConfig creates a new event bus with custom worker count and queue size
func NewWithConfig(workerCount, queueSize int) *Bus {
	b := &Bus{
		handlers:  make(map[EventType][]Handler),
		workQueue: make(chan work, queueSize),
		closing:   make(chan struct{}),
	}

	// Start worker pool
	for i := 0; i < workerCount; i++ {
		b.wg.Add(1)
		go b.worker(i)
	}

	log.Debug().Int("workers", workerCount).Int("queue_size", queueSize).Msg("Event bus worker pool started")
	return b
}

// worker processes events from the work queue
func (b *Bus) worker(id int) {
	defer b.wg.Done()

	for w := range b.workQueue {
		func() {
			defer func() {
				if r := recover(); r != nil {
					log.Error().
						Interface("panic", r).
						Str("event_type", string(w.event.Type)).
						Int("worker", id).
						Msg("Event handler panicked")
				}
			}()
			w.handler(w.event)
		}()
	}
}

// Subscribe registers a handler for a specific event type
func (b *Bus) Subscribe(eventType EventType, handler Handler) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.handlers[eventType] = append(b.handlers[eventType], handler)
}

// Publish sends an event to all subscribed handlers.
// Non-blocking: if the work queue is full or bus is closing, events are dropped.
// Uses channel-based signaling for race-free shutdown detection.
func (b *Bus) Publish(event Event) {
	b.mu.RLock()
	handlers := b.handlers[event.Type]
	b.mu.RUnlock()

	for _, handler := range handlers {
		select {
		case <-b.closing:
			log.Warn().Str("event_type", string(event.Type)).Msg("Event bus closing, dropping event")
			return
		case b.workQueue <- work{event: event, handler: handler}:
			// Successfully queued
		default:
			// Queue full - drop event with warning
			log.Warn().
				Str("event_type", string(event.Type)).
				Msg("Event bus queue full, dropping event")
		}
	}
}

// Close shuts down the worker pool gracefully.
// First signals publishers to stop, then closes the work queue and waits for workers.
func (b *Bus) Close(ctx context.Context) {
	// Signal publishers to stop sending
	b.closeOnce.Do(func() {
		close(b.closing)
	})

	// Now it's safe to close the work queue - no new sends after closing is signaled
	close(b.workQueue)

	// Wait for workers to finish with timeout
	done := make(chan struct{})
	go func() {
		b.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		log.Debug().Msg("Event bus workers stopped gracefully")
	case <-ctx.Done():
		log.Warn().Msg("Event bus shutdown timed out, some events may be lost")
	}
}

// Clear removes all handlers
func (b *Bus) Clear() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.handlers = make(map[EventType][]Handler)
}
