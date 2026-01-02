package app

import (
	"context"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/dokzlo13/lightd/internal/config"
	"github.com/dokzlo13/lightd/internal/events"
	"github.com/dokzlo13/lightd/internal/geo"
	"github.com/dokzlo13/lightd/internal/scheduler"
	"github.com/dokzlo13/lightd/internal/storage"
)

// SchedulerService wraps the scheduler and related periodic tasks.
type SchedulerService struct {
	cfg       *config.Config
	Scheduler *scheduler.Scheduler
	ledger    *storage.Ledger
	enabled   bool
}

// NewSchedulerService creates a new SchedulerService.
func NewSchedulerService(
	cfg *config.Config,
	bus *events.Bus,
	l *storage.Ledger,
	geoCalc *geo.Calculator,
) *SchedulerService {
	enabled := cfg.Events.Scheduler.IsEnabled()
	geoCfg := cfg.Events.Scheduler.Geo

	var sched *scheduler.Scheduler
	if enabled {
		if geoCfg.IsEnabled() {
			// Full scheduler with astronomical time support
			sched = scheduler.New(bus, l, geoCalc, geoCfg.Name, geoCfg.GetTimezone())
		} else {
			// Fixed-time only scheduler (no geo required)
			sched = scheduler.NewWithFixedTimeOnly(bus, l, geoCfg.GetTimezone())
			log.Info().Msg("Scheduler geo is disabled - astronomical times (@dawn, @noon, @sunset, etc.) are not available")
		}
	}

	return &SchedulerService{
		cfg:       cfg,
		Scheduler: sched,
		ledger:    l,
		enabled:   enabled,
	}
}

// IsEnabled returns whether the scheduler is enabled.
func (s *SchedulerService) IsEnabled() bool {
	return s.enabled
}

// Start begins the scheduler and related periodic tasks.
func (s *SchedulerService) Start(ctx context.Context) {
	if !s.enabled {
		log.Info().Msg("Scheduler is disabled")
		return
	}

	// Run boot recovery first
	s.Scheduler.RunBootRecovery()

	// Start scheduler
	go func() {
		if err := s.Scheduler.Run(ctx); err != nil {
			log.Error().Err(err).Msg("Scheduler error")
		}
	}()

	// Ledger cleanup (if ledger is enabled)
	if s.cfg.Ledger.IsEnabled() {
		go s.runLedgerCleanup(ctx)
	}
}

// runLedgerCleanup periodically cleans up old ledger entries.
func (s *SchedulerService) runLedgerCleanup(ctx context.Context) {
	retention := s.cfg.Ledger.GetRetentionPeriod()
	interval := s.cfg.Ledger.GetRetentionInterval()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			deleted, err := s.ledger.DeleteOlderThan(retention)
			if err != nil {
				log.Error().Err(err).Msg("Failed to cleanup old ledger entries")
			} else if deleted > 0 {
				log.Info().Int64("deleted", deleted).Dur("retention", retention).Msg("Cleaned up old ledger entries")
			}
		}
	}
}
