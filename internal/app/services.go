package app

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

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
	cfg        *config.Config
	cancel     context.CancelFunc
	ready      atomic.Bool
	stopOnce   sync.Once
	stopErr    error
	background sync.WaitGroup
	cleanups   []func()

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
	s.Ledger.SetHistoryEnabled(cfg.Ledger.IsEnabled())
	if err := s.Ledger.RecoverInterrupted(); err != nil {
		s.Close()
		return nil, err
	}

	// Initialize generic state store
	s.Store = storage.NewStore(database.DB)

	// Initialize geo calculator (config is under events.scheduler.geo)
	schedCfg := cfg.Events.Scheduler
	geoCfg := schedCfg.Geo
	if schedCfg.IsEnabled() && geoCfg.IsEnabled() {
		// Resolve location at boot (geocoding happens once here)
		var geoCache *storage.GeoCache
		if geoCfg.IsCacheEnabled() {
			geoCache = storage.NewGeoCache(database.DB)
		}
		geocoder := geo.NewGeocoder(geoCfg.GetHTTPTimeout(), geoCache)

		loc, err := geo.ResolveOnBoot(
			context.Background(),
			geocoder,
			geoCfg.Location,
			geoCfg.Lat,
			geoCfg.Lon,
			schedCfg.GetTimezone(),
		)
		if err != nil {
			s.Close()
			return nil, err
		}

		s.GeoCalc = geo.NewCalculator(loc)
	} else {
		// Geo/scheduler disabled, create calculator without location
		s.GeoCalc = geo.NewCalculator(nil)
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
		Bridge:       s.Hue.Client.Bridge(),
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

	s.Lua.Runtime.SetActionTimeout(cfg.GetActionTimeout())

	// Initialize health service
	s.Health = NewHealthService(cfg)
	s.Health.SetStatusProvider(s.Status)

	// Initialize webhook service
	s.Webhook = NewWebhookService(cfg, s.Hue.Bus)

	return s, nil
}

// Start starts all services in the correct order.
// The onFatalError callback is called when a fatal error occurs (e.g., max reconnects exceeded).
func (s *Services) Start(ctx context.Context, onFatalError func(error)) error {
	ctx, s.cancel = context.WithCancel(ctx)
	if err := s.Health.Start(ctx); err != nil {
		return err
	}
	// Connect to Hue bridge
	if err := s.Hue.Start(ctx); err != nil {
		return err
	}

	// Load Lua script before starting worker
	if err := s.Lua.LoadScript(); err != nil {
		return err
	}

	if s.Scheduler.Scheduler != nil {
		if err := s.Scheduler.Scheduler.ValidateActions(s.Invoker.HasAction); err != nil {
			return err
		}
	}

	// Register event handlers from modules (after Lua script is loaded)
	// SSE handlers (button, rotary, connectivity from Hue event stream)
	if s.cfg.Events.SSE.IsEnabled() {
		sseModule := s.Lua.GetSSEModule()
		s.cleanups = append(s.cleanups, sse.RegisterHandlers(ctx, sseModule, s.Hue.Bus, s.Invoker, s.Lua))
	}
	// Webhook handlers (HTTP webhook events)
	if s.cfg.Events.Webhook.Enabled {
		webhookModule := s.Lua.GetWebhookModule()
		s.cleanups = append(s.cleanups, webhook.RegisterHandlers(ctx, webhookModule, s.Hue.Bus, s.Invoker, s.Lua))
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
	if err := s.Scheduler.Start(ctx); err != nil {
		return err
	}
	if err := s.Webhook.Start(ctx); err != nil {
		return err
	}

	// Start KV cleanup goroutine
	s.KV.StartCleanup(ctx, s.cfg.KV.GetCleanupInterval())
	s.background.Add(1)
	go func() {
		defer s.background.Done()
		s.Ledger.RunRetention(ctx, s.cfg.Ledger.GetRetentionPeriod(), s.cfg.Ledger.GetRetentionInterval())
	}()
	s.ready.Store(true)
	s.background.Add(1)
	go func() {
		defer s.background.Done()
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := s.Scheduler.Err(); err != nil {
					if onFatalError != nil {
						onFatalError(fmt.Errorf("scheduler: %w", err))
					}
					return
				}
				if !s.Lua.Status().Ready {
					if onFatalError != nil {
						onFatalError(errors.New("Lua worker stopped unexpectedly"))
					}
					return
				}
			}
		}
	}()
	return nil
}

// Status exposes admission health separately from bridge dependency degradation.
func (s *Services) Status(ctx context.Context) (bool, any) {
	detail := map[string]any{}
	ready := s.ready.Load()
	if s.DB == nil {
		ready = false
	} else if err := s.DB.PingContext(ctx); err != nil {
		ready = false
		detail["database_error"] = err.Error()
	}
	if s.Ledger != nil {
		if err := s.Ledger.HealthError(); err != "" {
			ready = false
			detail["storage_error"] = err
		}
	}
	if s.Lua != nil {
		status := s.Lua.Status()
		detail["lua"] = status
		ready = ready && status.Ready
	}
	if s.Hue != nil {
		detail["bridge"] = s.Hue.Client.Status()
		detail["sse"] = s.Hue.EventStream.Status()
		reconciliation := s.Hue.Orchestrator.Status()
		detail["reconciliation"] = reconciliation
		if reconciliation.PersistenceError != "" {
			ready = false
		}
		detail["rejected_events"] = s.Hue.Bus.Rejected()
	}
	if s.Scheduler != nil {
		if err := s.Scheduler.Err(); err != nil {
			ready = false
			detail["scheduler_error"] = err.Error()
		}
	}
	return ready, detail
}

func (s *Services) ClearState() error {
	if err := s.Store.Clear(""); err != nil {
		return err
	}
	if s.Hue != nil {
		if err := s.Hue.Client.ClearAuthority(); err != nil {
			return err
		}
		return s.Hue.Orchestrator.ClearDelivery()
	}
	return nil
}

// Stop joins every owner before releasing SQLite and the VM. If a worker cannot
// join within the total budget, leave its resources intact and return a fatal error.
func (s *Services) Stop() error {
	s.stopOnce.Do(func() {
		s.ready.Store(false)
		if s.cancel != nil {
			s.cancel()
		}
		ctx, cancel := context.WithTimeout(context.Background(), s.cfg.GetShutdownTimeout())
		defer cancel()
		var errs []error
		if s.Webhook != nil {
			errs = append(errs, s.Webhook.Close(ctx))
		}
		if s.Scheduler != nil {
			errs = append(errs, s.Scheduler.Stop(ctx))
		}
		if s.Hue != nil {
			errs = append(errs, s.Hue.Stop(ctx))
			s.Hue.Bus.Close(ctx)
		}
		for _, cleanup := range s.cleanups {
			cleanup()
		}
		if s.Lua != nil {
			errs = append(errs, s.Lua.Stop(ctx))
		}
		if s.KV != nil {
			s.KV.StopCleanup()
		}
		done := make(chan struct{})
		go func() { s.background.Wait(); close(done) }()
		select {
		case <-done:
		case <-ctx.Done():
			errs = append(errs, ctx.Err())
		}
		if s.Health != nil {
			errs = append(errs, s.Health.Close(ctx))
		}
		if ctx.Err() != nil {
			errs = append(errs, ctx.Err())
		}
		s.stopErr = errors.Join(errs...)
		if s.stopErr == nil && s.DB != nil {
			s.stopErr = s.DB.Close()
		}
	})
	return s.stopErr
}
func (s *Services) Close() { _ = s.Stop() }
