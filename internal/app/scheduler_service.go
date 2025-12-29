package app

import (
	"context"
	"database/sql"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/dokzlo13/lightd/internal/actions"
	"github.com/dokzlo13/lightd/internal/config"
	"github.com/dokzlo13/lightd/internal/geo"
	"github.com/dokzlo13/lightd/internal/ledger"
	"github.com/dokzlo13/lightd/internal/scheduler"
)

// SchedulerService wraps the scheduler and related periodic tasks.
type SchedulerService struct {
	cfg       *config.Config
	Scheduler *scheduler.Scheduler
	ledger    *ledger.Ledger
}

// NewSchedulerService creates a new SchedulerService.
func NewSchedulerService(
	cfg *config.Config,
	db *sql.DB,
	invoker *actions.Invoker,
	l *ledger.Ledger,
	geoCalc *geo.Calculator,
) (*SchedulerService, error) {
	defStore := scheduler.NewDefinitionStore(db)
	occStore := scheduler.NewOccurrenceStore(db)

	sched := scheduler.New(defStore, occStore, invoker, l, geoCalc, cfg.Geo.Name, cfg.Geo.Timezone)

	return &SchedulerService{
		cfg:       cfg,
		Scheduler: sched,
		ledger:    l,
	}, nil
}

// SetLuaInvoker sets up the scheduler to route action invocations through the Lua worker.
func (s *SchedulerService) SetLuaInvoker(luaSvc *LuaService) {
	s.Scheduler.SetLuaInvoker(luaSvc.InvokeThroughLua)
}

// Start begins the scheduler and related periodic tasks.
func (s *SchedulerService) Start(ctx context.Context) {
	// Start scheduler
	go func() {
		if err := s.Scheduler.Run(ctx); err != nil {
			log.Error().Err(err).Msg("Scheduler error")
		}
	}()

	// Periodic schedule printing
	go s.runSchedulePrinter(ctx)

	// Ledger cleanup
	go s.runLedgerCleanup(ctx)
}

// runSchedulePrinter periodically prints the current schedule.
func (s *SchedulerService) runSchedulePrinter(ctx context.Context) {
	printInterval := s.cfg.Log.PrintSchedule.Duration()
	if printInterval == 0 {
		printInterval = 30 * time.Minute
	}

	// Initial print after 1 second
	select {
	case <-ctx.Done():
		return
	case <-time.After(1 * time.Second):
		log.Info().Msg("Current schedule:\n" + s.Scheduler.FormatSchedule())
	}

	ticker := time.NewTicker(printInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			log.Info().Msg("Current schedule:\n" + s.Scheduler.FormatSchedule())
		}
	}
}

// runLedgerCleanup periodically cleans up old ledger entries.
func (s *SchedulerService) runLedgerCleanup(ctx context.Context) {
	retention := time.Duration(s.cfg.Ledger.RetentionDays) * 24 * time.Hour

	ticker := time.NewTicker(s.cfg.Ledger.CleanupInterval.Duration())
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
				log.Info().Int64("deleted", deleted).Int("retention_days", s.cfg.Ledger.RetentionDays).Msg("Cleaned up old ledger entries")
			}
		}
	}
}
