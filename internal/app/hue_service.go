package app

import (
	"context"

	"github.com/rs/zerolog/log"

	"github.com/dokzlo13/lightd/internal/config"
	"github.com/dokzlo13/lightd/internal/eventbus"
	"github.com/dokzlo13/lightd/internal/hue"
	"github.com/dokzlo13/lightd/internal/reconcile"
	"github.com/dokzlo13/lightd/internal/state"
)

// HueService wraps all Hue-related components: client, cache, event stream, and reconciler.
type HueService struct {
	cfg *config.Config

	Client      *hue.Client
	GroupCache  *hue.GroupCache
	ActualState *ActualStateAdapter
	EventStream *hue.EventStream
	Reconciler  *reconcile.Reconciler
	Bus         *eventbus.Bus
}

// NewHueService creates a new HueService with all components initialized but not connected.
func NewHueService(cfg *config.Config, desiredStore *state.DesiredStore) (*HueService, error) {
	// Initialize Hue client with configured timeout
	client := hue.NewClient(cfg.Hue.Bridge, cfg.Hue.Token, cfg.Hue.Timeout.Duration())

	// Initialize pure cache (no client reference - SRP)
	groupCache := hue.NewGroupCache(cfg.Cache.RefreshInterval.Duration())

	// Initialize actual state adapter (combines client + cache)
	actualState := NewActualStateAdapter(client, groupCache)

	// Initialize reconciler with actual state adapter
	reconciler := reconcile.New(
		client,
		actualState,
		desiredStore,
		cfg.Reconciler.PeriodicInterval.Duration(),
		cfg.Reconciler.RateLimitRPS,
	)

	// Initialize event bus
	bus := eventbus.NewWithConfig(cfg.EventBus.GetWorkers(), cfg.EventBus.GetQueueSize())

	// Initialize event stream with retry configuration
	eventStreamConfig := hue.EventStreamConfig{
		MinBackoff:    cfg.Hue.MinRetryBackoff.Duration(),
		MaxBackoff:    cfg.Hue.MaxRetryBackoff.Duration(),
		Multiplier:    cfg.Hue.RetryMultiplier,
		MaxReconnects: cfg.Hue.MaxReconnects,
	}
	eventStream := hue.NewEventStreamWithConfig(client, eventStreamConfig)

	return &HueService{
		cfg:         cfg,
		Client:      client,
		GroupCache:  groupCache,
		ActualState: actualState,
		EventStream: eventStream,
		Reconciler:  reconciler,
		Bus:         bus,
	}, nil
}

// Start connects to the Hue bridge.
func (s *HueService) Start(ctx context.Context) error {
	if err := s.Client.Connect(ctx); err != nil {
		return err
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
			if err == hue.ErrMaxReconnectsExceeded {
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
