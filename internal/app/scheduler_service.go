package app

import (
	"context"
	"fmt"
	"sync"

	"github.com/dokzlo13/lightd/internal/config"
	"github.com/dokzlo13/lightd/internal/events"
	"github.com/dokzlo13/lightd/internal/geo"
	"github.com/dokzlo13/lightd/internal/scheduler"
	"github.com/dokzlo13/lightd/internal/storage"
)

type SchedulerService struct {
	Scheduler *scheduler.Scheduler
	enabled   bool
	mu        sync.Mutex
	cancel    context.CancelFunc
	done      chan struct{}
	runErr    error
}

func NewSchedulerService(cfg *config.Config, bus *events.Bus, ledger *storage.Ledger, calc *geo.Calculator) *SchedulerService {
	c := cfg.Events.Scheduler
	s := &SchedulerService{enabled: c.IsEnabled()}
	if s.enabled {
		if c.Geo.IsEnabled() {
			s.Scheduler = scheduler.New(bus, ledger, calc, c.GetTimezone())
		} else {
			s.Scheduler = scheduler.NewWithFixedTimeOnly(bus, ledger, c.GetTimezone())
		}
	}
	return s
}
func (s *SchedulerService) IsEnabled() bool { return s.enabled }

// Start reports recovery admission failures instead of claiming successful startup.
func (s *SchedulerService) Start(ctx context.Context) error {
	if !s.enabled {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.done != nil {
		return fmt.Errorf("scheduler already started")
	}
	if err := s.Scheduler.RunBootRecovery(); err != nil {
		return fmt.Errorf("scheduler boot recovery: %w", err)
	}
	runCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	s.done = make(chan struct{})
	go func() { err := s.Scheduler.Run(runCtx); s.mu.Lock(); s.runErr = err; s.mu.Unlock(); close(s.done) }()
	return nil
}
func (s *SchedulerService) Stop(ctx context.Context) error {
	s.mu.Lock()
	done := s.done
	cancel := s.cancel
	s.mu.Unlock()
	if done == nil {
		return nil
	}
	cancel()
	select {
	case <-done:
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.runErr
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (s *SchedulerService) Err() error { s.mu.Lock(); defer s.mu.Unlock(); return s.runErr }
