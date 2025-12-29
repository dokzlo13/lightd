package app

import (
	"context"

	"github.com/rs/zerolog/log"

	"github.com/dokzlo13/lightd/internal/config"
	"github.com/dokzlo13/lightd/internal/eventbus"
	"github.com/dokzlo13/lightd/internal/hue"
	v2 "github.com/dokzlo13/lightd/internal/hue/v2"
	"github.com/dokzlo13/lightd/internal/reconcile"
	"github.com/dokzlo13/lightd/internal/state"
)

// HueService wraps all Hue-related components: client, cache, event stream, and reconciler.
type HueService struct {
	cfg *config.Config

	Client      *hue.Client
	GroupCache  *hue.GroupCache
	SceneCache  *hue.SceneCache
	ActualState *ActualStateAdapter
	EventStream *v2.EventStream
	Reconciler  *reconcile.Reconciler
	Bus         *eventbus.Bus
}

// NewHueService creates a new HueService with all components initialized but not connected.
func NewHueService(cfg *config.Config, desiredStore *state.DesiredStore) (*HueService, error) {
	// Initialize Hue client (holder for V1/V2 clients with shared HTTP config)
	client := hue.NewClient(cfg.Hue.Bridge, cfg.Hue.Token, cfg.Hue.Timeout.Duration())

	// Initialize pure caches
	groupCache := hue.NewGroupCache(cfg.Cache.RefreshInterval.Duration())
	sceneCache := hue.NewSceneCache(client.V1())

	// Initialize actual state adapter (uses huego bridge + cache)
	actualState := NewActualStateAdapter(client.V1(), groupCache)

	// Initialize reconciler with huego bridge, scene cache, and actual state adapter
	reconciler := reconcile.New(
		client.V1(),
		sceneCache,
		actualState,
		desiredStore,
		cfg.Reconciler.PeriodicInterval.Duration(),
		cfg.Reconciler.RateLimitRPS,
	)

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
		cfg:         cfg,
		Client:      client,
		GroupCache:  groupCache,
		SceneCache:  sceneCache,
		ActualState: actualState,
		EventStream: eventStream,
		Reconciler:  reconciler,
		Bus:         bus,
	}, nil
}

// Start connects to the Hue bridge and preloads caches.
func (s *HueService) Start(ctx context.Context) error {
	if err := s.Client.Connect(ctx); err != nil {
		return err
	}

	// Preload scene cache
	if _, err := s.SceneCache.Refresh(); err != nil {
		log.Warn().Err(err).Msg("Failed to preload scene cache")
	}

	log.Info().Str("bridge", s.cfg.Hue.Bridge).Msg("Connected to Hue bridge")
	return nil
}

// StartBackground starts all background goroutines (event stream, reconciler).
// The optional onFatalError callback is called when a fatal error occurs (e.g., max reconnects exceeded).
func (s *HueService) StartBackground(ctx context.Context, onFatalError func(error)) {
	// Start event stream listener
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

	// Start reconciler
	go func() {
		if err := s.Reconciler.Run(ctx); err != nil {
			log.Error().Err(err).Msg("Reconciler error")
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
