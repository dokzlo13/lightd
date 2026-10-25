package app

import (
	"context"
	"errors"
	"github.com/dokzlo13/lightd/internal/config"
	"github.com/dokzlo13/lightd/internal/storage"
	"testing"
	"time"
)

func TestStopBudgetDoesNotCloseDatabaseUnderBlockedCleanup(t *testing.T) {
	db, err := storage.Open(t.TempDir() + "/state.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	release := make(chan struct{})
	finished := make(chan struct{})
	s := &Services{cfg: &config.Config{ShutdownTimeout: config.Duration(20 * time.Millisecond)}, DB: db, cleanups: []func(){func() { defer close(finished); <-release }}}
	result := make(chan error, 1)
	go func() { result <- s.Stop() }()
	select {
	case err := <-result:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("stop: %v", err)
		}
	case <-time.After(time.Second):
		close(release)
		t.Fatal("cleanup exceeded total shutdown budget")
	}
	if err := db.Ping(); err != nil {
		t.Fatal("database closed while cleanup still owned it")
	}
	close(release)
	<-finished
	if !errors.Is(s.Stop(), context.DeadlineExceeded) {
		t.Fatal("repeated Stop lost fatal outcome")
	}
}
func TestJoinWithinStopsWaitingAtDeadline(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	called := false
	if !errors.Is(joinWithin(ctx, func() { called = true }), context.Canceled) {
		t.Fatal("expected cancellation")
	}
	if called {
		t.Fatal("started new cleanup after deadline")
	}
}
