package app

import (
	"context"

	"github.com/rs/zerolog/log"

	"github.com/dokzlo13/lightd/internal/actions"
	"github.com/dokzlo13/lightd/internal/config"
	"github.com/dokzlo13/lightd/internal/events/schedule"
	"github.com/dokzlo13/lightd/internal/events/sse"
	"github.com/dokzlo13/lightd/internal/events/webhook"
	"github.com/dokzlo13/lightd/internal/geo"
	"github.com/dokzlo13/lightd/internal/lua"
	"github.com/dokzlo13/lightd/internal/storage"
	"github.com/dokzlo13/lightd/internal/storage/kv"
)

// Services is a container for all application services.
// It manages service initialization order and dependencies.
type Services struct {
	cfg *config.Config

	// Core infrastructure
	DB     *storage.DB
	Ledger *storage.Ledger

	// State store (generic JSON store)
	Store   *storage.Store
	GeoCalc *geo.Calculator

	// KV storage
	KV *kv.Manager

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
	database, err := storage.Open(cfg.Database.GetPath())
	if err != nil {
		return nil, err
	}
	s.DB = database

	// Initialize ledger
	s.Ledger = storage.NewLedger(database.DB)

	// Initialize generic state store
	s.Store = storage.NewStore(database.DB)

	// Initialize geo calculator (config is under events.scheduler.geo)
	geoCfg := cfg.Events.Scheduler.Geo
	geoCache := storage.NewGeoCache(database.DB)
	if geoCfg.Lat != 0 || geoCfg.Lon != 0 {
		s.GeoCalc = geo.NewCalculatorWithLocationAndCache(
			geoCfg.Name,
			geoCfg.Lat,
			geoCfg.Lon,
			geoCfg.GetTimezone(),
			geoCfg.GetHTTPTimeout(),
			geoCache,
		)
	} else {
		log.Warn().Msg("No lat/lon configured, will use Nominatim geocoding (cached in SQLite)")
		s.GeoCalc = geo.NewCalculatorWithCache(geoCfg.GetHTTPTimeout(), geoCache)
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

	// Initialize KV manager
	s.KV = kv.NewManager(database.DB)

	// Initialize Lua service
	luaDeps := lua.RuntimeDeps{
		Config:       cfg,
		Registry:     s.Registry,
		Invoker:      s.Invoker,
		Scheduler:    s.Scheduler.Scheduler,
		Bridge:       s.Hue.Client.V1(),
		SceneIndex:   s.Hue.SceneIndex,
		Stores:       s.Hue.Stores,
		Orchestrator: s.Hue.Orchestrator,
		GeoCalc:      s.GeoCalc,
		KVManager:    s.KV,
	}

	s.Lua, err = NewLuaService(luaDeps)
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
		sseModule := s.Lua.GetSSEModule()
		sse.RegisterHandlers(ctx, sseModule, s.Hue.Bus, s.Invoker, s.Lua)
	}
	// Webhook handlers (HTTP webhook events)
	if s.cfg.Events.Webhook.Enabled {
		webhookModule := s.Lua.GetWebhookModule()
		webhook.RegisterHandlers(ctx, webhookModule, s.Hue.Bus, s.Invoker, s.Lua)
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

	// Start KV cleanup goroutine
	s.KV.StartCleanup(ctx, s.cfg.KV.GetCleanupInterval())

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
	if s.KV != nil {
		s.KV.StopCleanup()
	}
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
