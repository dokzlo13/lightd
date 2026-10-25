package sse

import (
	"context"
	"github.com/dokzlo13/lightd/internal/actions"
	"github.com/dokzlo13/lightd/internal/events"
	"github.com/dokzlo13/lightd/internal/storage"
	lua "github.com/yuin/gopher-lua"
	"path/filepath"
	"sync"
	"testing"
)

type rebindRegistry struct {
	mu      sync.Mutex
	handler *ButtonHandler
	changed func()
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (r *rebindRegistry) FindButtonHandler(string, string) *ButtonHandler {
	r.mu.Lock()
	h := *r.handler
	r.mu.Unlock()
	r.once.Do(func() { close(r.entered); <-r.release })
	return &h
}
func (r *rebindRegistry) FindConnectivityHandler(string, string) *ConnectivityHandler  { return nil }
func (r *rebindRegistry) FindRotaryHandler(string) *RotaryHandler                      { return nil }
func (r *rebindRegistry) FindLightChangeHandlers(string, string) []*LightChangeHandler { return nil }
func (r *rebindRegistry) SetOnHandlersChanged(fn func())                               { r.changed = fn }

type testExecutor struct{ work func(context.Context) }

func (x *testExecutor) Do(_ context.Context, fn func(context.Context)) bool { x.work = fn; return true }
func (x *testExecutor) LState() *lua.LState                                 { return nil }
func (x *testExecutor) Reserve(context.Context) (actions.Reservation, error) {
	return &testReservation{x}, nil
}

type testReservation struct{ x *testExecutor }

func (r *testReservation) Commit(fn func(context.Context)) error { r.x.work = fn; return nil }
func (r *testReservation) Release()                              {}
func TestRebindBetweenLookupAndCacheCreationDoesNotPoisonCache(t *testing.T) {
	db, e := storage.Open(filepath.Join(t.TempDir(), "test.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	registry := actions.NewRegistry()
	oldCalls, newCalls := 0, 0
	registry.RegisterSimple("old", func(*actions.Context, map[string]any) error { oldCalls++; return nil })
	registry.RegisterSimple("new", func(*actions.Context, map[string]any) error { newCalls++; return nil })
	invoker := actions.NewInvoker(registry, storage.NewLedger(db.DB), func(ctx context.Context) *actions.Context { return actions.NewContext(ctx, nil, nil, nil, nil) })
	handlers := &rebindRegistry{handler: &ButtonHandler{ActionName: "old"}, entered: make(chan struct{}), release: make(chan struct{})}
	bus := events.NewBus()
	defer bus.Close(context.Background())
	x := &testExecutor{}
	cleanup := RegisterHandlers(context.Background(), handlers, bus, invoker, x)
	defer cleanup()
	published := make(chan error, 1)
	go func() {
		published <- bus.Publish(events.Event{Type: events.EventTypeButton, Data: map[string]any{"resource_id": "fixture", "action": "short_release", "event_id": "first"}})
	}()
	<-handlers.entered
	handlers.mu.Lock()
	handlers.handler = &ButtonHandler{ActionName: "new"}
	handlers.mu.Unlock()
	handlers.changed()
	close(handlers.release)
	if e := <-published; e == nil {
		t.Fatal("stale binding admitted")
	}
	if e := bus.Publish(events.Event{Type: events.EventTypeButton, Data: map[string]any{"resource_id": "fixture", "action": "short_release", "event_id": "second"}}); e != nil {
		t.Fatal(e)
	}
	if x.work == nil {
		t.Fatal("new handler not admitted")
	}
	x.work(context.Background())
	if oldCalls != 0 || newCalls != 1 {
		t.Fatal(oldCalls, newCalls)
	}
	cleanup()
}
