package app

import (
	"context"
	"database/sql"

	"github.com/rs/zerolog/log"

	"github.com/dokzlo13/lightd/internal/cache"
	"github.com/dokzlo13/lightd/internal/config"
	"github.com/dokzlo13/lightd/internal/eventbus"
	"github.com/dokzlo13/lightd/internal/hue"
	v2 "github.com/dokzlo13/lightd/internal/hue/v2"
	"github.com/dokzlo13/lightd/internal/reconcile"
	"github.com/dokzlo13/lightd/internal/reconcile/group"
	"github.com/dokzlo13/lightd/internal/reconcile/light"
	"github.com/dokzlo13/lightd/internal/state"
	"github.com/dokzlo13/lightd/internal/stores"
)

// HueService wraps all Hue-related components: client, cache, event stream, and orchestrator.
type HueService struct {
	cfg *config.Config

	Client       *hue.Client
	SceneIndex   *cache.SceneIndex
	EventStream  *v2.EventStream
	Orchestrator *reconcile.Orchestrator
	Bus          *eventbus.Bus
	Stores       *stores.Registry

	// Resource providers
	GroupProvider *group.Provider
	LightProvider *light.Provider
}

// NewHueService creates a new HueService with all components initialized but not connected.
func NewHueService(cfg *config.Config, db *sql.DB, store *state.Store) (*HueService, error) {
	// Initialize Hue client (holder for V1/V2 clients with shared HTTP config)
	client := hue.NewClient(cfg.Hue.Bridge, cfg.Hue.Token, cfg.Hue.Timeout.Duration())

	// Initialize scene index (pure index, caller loads data)
	sceneIndex := cache.NewSceneIndex()

	// Create store registry (centralized typed stores)
	storeRegistry := stores.NewRegistry(store)

	// Create actual state providers (no caching - always fetch from bridge)
	groupActualProvider := group.NewActualProvider(client.V1())
	lightActualProvider := light.NewActualProvider(client.V1())

	// Create appliers
	groupApplier := group.NewHueApplier(client.V1(), sceneIndex)
	lightApplier := light.NewHueApplier(client.V1())

	// Create resource providers
	groupProvider := group.NewProvider(storeRegistry.Groups(), groupActualProvider, groupApplier)
	lightProvider := light.NewProvider(storeRegistry.Lights(), lightActualProvider, lightApplier)

	// Initialize orchestrator
	orchestrator := reconcile.NewOrchestrator(
		cfg.Reconciler.PeriodicInterval.Duration(),
		cfg.Reconciler.RateLimitRPS,
	)
	orchestrator.Register(groupProvider)
	orchestrator.Register(lightProvider)

	// Initialize event bus
	bus := eventbus.NewWithConfig(cfg.EventBus.GetWorkers(), cfg.EventBus.GetQueueSize())

	// Initialize event stream with V2 client and retry configuration
	eventStreamConfig := v2.EventStreamConfig{
		MinBackoff:    cfg.Hue.MinRetryBackoff.Duration(),
		MaxBackoff:    cfg.Hue.MaxRetryBackoff.Duration(),
		Multiplier:    cfg.Hue.RetryMultiplier,
		MaxReconnects: cfg.Hue.MaxReconnects,
	}
	eventStream := v2.NewEventStreamWithConfig(client.V2(), eventStreamConfig)

	return &HueService{
		cfg:           cfg,
		Client:        client,
		SceneIndex:    sceneIndex,
		EventStream:   eventStream,
		Orchestrator:  orchestrator,
		Bus:           bus,
		Stores:        storeRegistry,
		GroupProvider: groupProvider,
		LightProvider: lightProvider,
	}, nil
}

// Start connects to the Hue bridge and preloads caches.
func (s *HueService) Start(ctx context.Context) error {
	if err := s.Client.Connect(ctx); err != nil {
		return err
	}

	// Fetch and load scenes into index
	scenes, err := s.Client.V1().GetScenes()
	if err != nil {
		log.Warn().Err(err).Msg("Failed to fetch scenes")
	} else {
		s.SceneIndex.Load(scenes)
		log.Info().Int("count", len(scenes)).Msg("Loaded scenes into index")
	}

	log.Info().Str("bridge", s.cfg.Hue.Bridge).Msg("Connected to Hue bridge")
	return nil
}

// StartBackground starts all background goroutines (event stream, orchestrator).
// The optional onFatalError callback is called when a fatal error occurs (e.g., max reconnects exceeded).
func (s *HueService) StartBackground(ctx context.Context, onFatalError func(error)) {
	// Start event stream listener only if SSE is enabled
	if s.cfg.SSE.IsEnabled() {
		go func() {
			if err := s.EventStream.Run(ctx, s.Bus); err != nil {
				if err == v2.ErrMaxReconnectsExceeded {
					log.Error().Msg("Event stream: max reconnects exceeded, triggering shutdown")
					if onFatalError != nil {
						onFatalError(err)
					}
				} else {
					log.Error().Err(err).Msg("Event stream error")
				}
			}
		}()
	} else {
		log.Info().Msg("SSE event stream disabled")
	}

	// Start orchestrator
	go func() {
		if err := s.Orchestrator.Run(ctx); err != nil {
			log.Error().Err(err).Msg("Orchestrator error")
		}
	}()
}

// Close releases all resources.
func (s *HueService) Close() {
	if s.Bus != nil {
		ctx, cancel := context.WithTimeout(context.Background(), s.cfg.ShutdownTimeout.Duration())
		defer cancel()
		s.Bus.Close(ctx)
	}
	if s.Client != nil {
		s.Client.Close()
	}
}
