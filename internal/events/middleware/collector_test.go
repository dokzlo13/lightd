package middleware

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestCountRetainsRejectedBatchAndRetries(t *testing.T) {
	var attempts atomic.Int32
	done := make(chan int, 1)
	c := NewCountCollector(2, func(batch []map[string]any) error {
		if attempts.Add(1) == 1 {
			return errors.New("queue full")
		}
		done <- len(batch)
		return nil
	})
	defer c.Close()
	if e := c.AddEvent(map[string]any{"id": 1}); e != nil {
		t.Fatal(e)
	}
	if e := c.AddEvent(map[string]any{"id": 2}); e != nil {
		t.Fatal("buffered input rejected after retention", e)
	}
	select {
	case n := <-done:
		if n != 2 {
			t.Fatal(n)
		}
	case <-time.After(time.Second):
		t.Fatal("no retry")
	}
}
func TestQuietCloseCancelsAndRejectsNewInput(t *testing.T) {
	var called atomic.Int32
	var discarded atomic.Int32
	c := NewQuietCollector(30, func([]map[string]any) error { called.Add(1); return nil })
	OnDiscard(c, func(batch []map[string]any) { discarded.Add(int32(len(batch))) })
	c.AddEvent(map[string]any{"id": 1})
	c.Close()
	c.Close()
	time.Sleep(50 * time.Millisecond)
	if called.Load() != 0 || discarded.Load() != 1 {
		t.Fatal(called.Load(), discarded.Load())
	}
	if !errors.Is(c.AddEvent(nil), ErrCollectorClosed) {
		t.Fatal("accepted after close")
	}
}
func TestCollectorRejectsOversizeBeforeBuffering(t *testing.T) {
	c := NewQuietCollector(1000, func([]map[string]any) error { return nil })
	defer c.Close()
	if !errors.Is(c.AddEvent(map[string]any{"body": string(make([]byte, 3<<20))}), ErrCollectorFull) {
		t.Fatal("oversized event accepted")
	}
}
