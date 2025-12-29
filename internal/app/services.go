package app

import (
	"context"

	"github.com/rs/zerolog/log"

	"github.com/dokzlo13/lightd/internal/actions"
	"github.com/dokzlo13/lightd/internal/config"
	"github.com/dokzlo13/lightd/internal/db"
	"github.com/dokzlo13/lightd/internal/geo"
	"github.com/dokzlo13/lightd/internal/ledger"
	"github.com/dokzlo13/lightd/internal/state"
)

// Services is a container for all application services.
// It manages service initialization order and dependencies.
type Services struct {
	cfg *config.Config

	// Core infrastructure
	DB     *db.DB
	Ledger *ledger.Ledger

	// Domain stores
	DesiredStore *state.DesiredStore
	GeoCalc      *geo.Calculator

	// Action system
	Registry *actions.Registry
	Invoker  *actions.Invoker

	// High-level services
	Hue       *HueService
	Lua       *LuaService
	Scheduler *SchedulerService
	Health    *HealthService
	Webhook   *WebhookService
}

// NewServices creates all services with proper dependency injection.
func NewServices(cfg *config.Config) (*Services, error) {
	s := &Services{cfg: cfg}

	// Initialize database
	database, err := db.Open(cfg.Database.Path)
	if err != nil {
		return nil, err
	}
	s.DB = database

	// Initialize ledger
	s.Ledger = ledger.New(database.DB)

	// Initialize desired state store
	s.DesiredStore = state.NewDesiredStore(database.DB)

	// Initialize geo calculator
	geoCache := geo.NewCache(database.DB)
	if cfg.Geo.Lat != 0 || cfg.Geo.Lon != 0 {
		s.GeoCalc = geo.NewCalculatorWithLocationAndCache(
			cfg.Geo.Name,
			cfg.Geo.Lat,
			cfg.Geo.Lon,
			cfg.Geo.Timezone,
			cfg.Geo.HTTPTimeout.Duration(),
			geoCache,
		)
	} else {
		log.Warn().Msg("No lat/lon configured, will use Nominatim geocoding (cached in SQLite)")
		s.GeoCalc = geo.NewCalculatorWithCache(cfg.Geo.HTTPTimeout.Duration(), geoCache)
	}

	// Initialize action registry
	s.Registry = actions.NewRegistry()

	// Initialize Hue service
	s.Hue, err = NewHueService(cfg, s.DesiredStore)
	if err != nil {
		s.Close()
		return nil, err
	}

	// Create invoker context factory using ActualStateAdapter
	ctxFactory := func(ctx context.Context) *actions.Context {
		return actions.NewContext(
			ctx,
			s.Hue.ActualState, // implements actions.ActualState interface
			s.DesiredStore,
			s.Hue.Reconciler,
			nil,
		)
	}

	// Initialize action invoker
	s.Invoker = actions.NewInvoker(s.Registry, s.Ledger, ctxFactory)

	// Initialize scheduler service
	s.Scheduler, err = NewSchedulerService(cfg, database.DB, s.Invoker, s.Ledger, s.GeoCalc)
	if err != nil {
		s.Close()
		return nil, err
	}

	// Initialize Lua service (pass V1 bridge, caches, etc. separately for SRP)
	s.Lua, err = NewLuaService(cfg, s.Registry, s.Invoker, s.Scheduler.Scheduler, s.Hue.Client.V1(), s.Hue.GroupCache, s.Hue.SceneCache, s.DesiredStore, s.Hue.Reconciler, s.GeoCalc)
	if err != nil {
		s.Close()
		return nil, err
	}

	// Wire scheduler to use Lua invoker for thread safety
	s.Scheduler.SetLuaInvoker(s.Lua)

	// Initialize health service
	s.Health = NewHealthService(cfg)

	// Initialize webhook service
	s.Webhook = NewWebhookService(cfg, s.Hue.Bus)

	return s, nil
}

// Start starts all services in the correct order.
// The onFatalError callback is called when a fatal error occurs (e.g., max reconnects exceeded).
func (s *Services) Start(ctx context.Context, onFatalError func(error)) error {
	// Connect to Hue bridge
	if err := s.Hue.Start(ctx); err != nil {
		return err
	}

	// Load Lua script before starting worker
	if err := s.Lua.LoadScript(); err != nil {
		return err
	}

	// Recover orphaned actions
	if err := s.Invoker.RecoverOrphanedActions(ctx); err != nil {
		log.Warn().Err(err).Msg("Failed to recover orphaned actions")
	}

	// Register event handlers from modules (after Lua script is loaded)
	// SSE handlers (button, rotary, connectivity from Hue event stream)
	if s.cfg.SSE.IsEnabled() {
		s.Lua.GetSSEModule().RegisterHandlers(ctx, s.Hue.Bus, s.Invoker, s.Lua, s.DesiredStore)
	}
	// Webhook handlers (HTTP webhook events)
	if s.cfg.Webhook.Enabled {
		s.Lua.GetWebhookModule().RegisterHandlers(ctx, s.Hue.Bus, s.Invoker, s.Lua)
	}

	// Start all background services
	s.Lua.Start(ctx)
	s.Hue.StartBackground(ctx, onFatalError)
	s.Scheduler.Start(ctx)
	s.Health.Start(ctx)
	s.Webhook.Start(ctx)

	return nil
}

// Stop gracefully stops all services.
func (s *Services) Stop() error {
	s.Close()
	return nil
}

// Close releases all resources.
func (s *Services) Close() {
	if s.Lua != nil {
		s.Lua.Close()
	}
	if s.Hue != nil {
		s.Hue.Close()
	}
	if s.DB != nil {
		s.DB.Close()
	}
}
