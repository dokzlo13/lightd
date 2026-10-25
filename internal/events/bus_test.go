package events

import (
	"context"
	"sync"
	"testing"
)

func TestConcurrentClosePublishAndAckRejection(t *testing.T) {
	b := NewBus()
	b.SubscribeAck(EventTypeButton, func(Event) error { return ErrQueueFull })
	if b.Publish(Event{Type: EventTypeButton}) != ErrQueueFull {
		t.Fatal("ack lost")
	}
	var wg sync.WaitGroup
	for j := 0; j < 8; j++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for k := 0; k < 100; k++ {
				_ = b.Publish(Event{Type: EventTypeRotary})
			}
		}()
	}
	b.Close(context.Background())
	b.Close(context.Background())
	wg.Wait()
	if b.Publish(Event{}) != ErrClosed {
		t.Fatal("published after close")
	}
}
