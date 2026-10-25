package app

import (
	"context"
	"testing"
	"time"

	"github.com/dokzlo13/lightd/internal/config"
)

func TestSchedulerServiceJoinsBeforeReturningStop(t *testing.T) {
	cfg := &config.Config{}
	disabled := false
	cfg.Events.Scheduler.Geo.Enabled = &disabled
	service := NewSchedulerService(cfg, nil, nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := service.Start(ctx); err != nil {
		t.Fatal(err)
	}
	stopCtx, stopCancel := context.WithTimeout(context.Background(), time.Second)
	defer stopCancel()
	if err := service.Stop(stopCtx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-service.done:
	default:
		t.Fatal("Stop returned before worker joined")
	}
	if err := service.Stop(stopCtx); err != nil {
		t.Fatal("repeated stop", err)
	}
}
