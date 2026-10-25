package lua

import (
	"context"
	"errors"
	glua "github.com/yuin/gopher-lua"
	"testing"
	"time"
)

func bareRuntime() *Runtime {
	return &Runtime{L: glua.NewState(), workQueue: make(chan queuedWork, 4), space: make(chan struct{}, 1), done: make(chan struct{}), actionTimeout: time.Second}
}
func waitReady(t *testing.T, r *Runtime) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for !r.Status().Ready {
		if time.Now().After(deadline) {
			t.Fatal("worker not ready")
		}
		time.Sleep(time.Millisecond)
	}
}
func TestStopJoinsBeforeClosingVM(t *testing.T) {
	r := bareRuntime()
	go r.Run(context.Background())
	waitReady(t, r)
	entered := make(chan struct{})
	release := make(chan struct{})
	alive := make(chan bool, 1)
	r.Do(context.Background(), func(ctx context.Context) { close(entered); <-ctx.Done(); alive <- !r.L.IsClosed(); <-release })
	<-entered
	stopped := make(chan error, 1)
	go func() { stopped <- r.Stop(context.Background()) }()
	if !<-alive {
		t.Fatal("VM closed during work")
	}
	select {
	case <-stopped:
		t.Fatal("stop did not join")
	default:
	}
	close(release)
	if e := <-stopped; e != nil {
		t.Fatal(e)
	}
	if !r.L.IsClosed() {
		t.Fatal("VM not closed")
	}
	if r.Do(context.Background(), func(context.Context) {}) {
		t.Fatal("admitted after stop")
	}
	if e := r.Stop(context.Background()); e != nil {
		t.Fatal(e)
	}
}
func TestQueuePreservesContextAndSynchronousPanicReturns(t *testing.T) {
	r := bareRuntime()
	ctx, cancel := context.WithCancel(context.Background())
	seen := make(chan error, 1)
	if !r.Do(ctx, func(c context.Context) { seen <- c.Err() }) {
		t.Fatal("admission")
	}
	cancel()
	go r.Run(context.Background())
	waitReady(t, r)
	if !errors.Is(<-seen, context.Canceled) {
		t.Fatal("lost submission cancellation")
	}
	if e := r.DoSyncWithResult(context.Background(), func(context.Context) error { panic("fixture") }); e == nil {
		t.Fatal("panic not returned")
	}
	r.Stop(context.Background())
}
