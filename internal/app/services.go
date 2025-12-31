package app

import (
	"context"

	"github.com/rs/zerolog/log"

	"github.com/dokzlo13/lightd/internal/actions"
	"github.com/dokzlo13/lightd/internal/config"
	"github.com/dokzlo13/lightd/internal/db"
	"github.com/dokzlo13/lightd/internal/events/schedule"
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

	// State store (generic JSON store)
	Store   *state.Store
	GeoCalc *geo.Calculator

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

	// Initialize generic state store
	s.Store = state.NewStore(database.DB)

	// Initialize geo calculator (config is under events.scheduler.geo)
	geoCfg := cfg.Events.Scheduler.Geo
	geoCache := geo.NewCache(database.DB)
	if geoCfg.Lat != 0 || geoCfg.Lon != 0 {
		s.GeoCalc = geo.NewCalculatorWithLocationAndCache(
			geoCfg.Name,
			geoCfg.Lat,
			geoCfg.Lon,
			geoCfg.Timezone,
			geoCfg.HTTPTimeout.Duration(),
			geoCache,
		)
	} else {
		log.Warn().Msg("No lat/lon configured, will use Nominatim geocoding (cached in SQLite)")
		s.GeoCalc = geo.NewCalculatorWithCache(geoCfg.HTTPTimeout.Duration(), geoCache)
	}

	// Initialize action registry
	s.Registry = actions.NewRegistry()

	// Initialize Hue service (now takes store instead of DesiredStore)
	s.Hue, err = NewHueService(cfg, database.DB, s.Store)
	if err != nil {
		s.Close()
		return nil, err
	}

	// Create invoker context factory
	ctxFactory := func(ctx context.Context) *actions.Context {
		return actions.NewContext(
			ctx,
			s.Hue.GroupProvider.ActualProvider(),
			s.Hue.Stores.Groups(),
			s.Hue.Orchestrator,
			nil,
		)
	}

	// Initialize action invoker
	s.Invoker = actions.NewInvoker(s.Registry, s.Ledger, ctxFactory)

	// Initialize scheduler service (now uses EventBus instead of direct invocation)
	s.Scheduler = NewSchedulerService(cfg, s.Hue.Bus, s.Ledger, s.GeoCalc)

	// Initialize Lua service
	s.Lua, err = NewLuaService(cfg, s.Registry, s.Invoker, s.Scheduler.Scheduler, s.Hue.Client.V1(), s.Hue.SceneIndex, s.Hue.Stores, s.Hue.Orchestrator, s.GeoCalc)
	if err != nil {
		s.Close()
		return nil, err
	}

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

	// Register event handlers from modules (after Lua script is loaded)
	// SSE handlers (button, rotary, connectivity from Hue event stream)
	if s.cfg.Events.SSE.IsEnabled() {
		s.Lua.GetSSEModule().RegisterHandlers(ctx, s.Hue.Bus, s.Invoker, s.Lua, s.Hue.Stores.Groups())
	}
	// Webhook handlers (HTTP webhook events)
	if s.cfg.Events.Webhook.Enabled {
		webhookModule := s.Lua.GetWebhookModule()
		webhookModule.RegisterHandlers(ctx, s.Hue.Bus, s.Invoker, s.Lua)
		// Set path matcher for HTTP request validation
		s.Webhook.SetPathMatcher(webhookModule)
	}
	// Schedule handlers (scheduler events go through EventBus)
	if s.cfg.Events.Scheduler.IsEnabled() {
		schedule.RegisterHandler(ctx, s.Hue.Bus, s.Invoker, s.Lua)
	}

	// Start all background services
	s.Lua.Start(ctx)
	s.Hue.StartBackground(ctx, onFatalError)
	s.Scheduler.Start(ctx)
	s.Health.Start(ctx)
	s.Webhook.Start(ctx)

	return nil
}

// ClearState clears all resource state.
func (s *Services) ClearState() error {
	return s.Store.Clear("")
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
