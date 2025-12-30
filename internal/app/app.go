package app

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/rs/zerolog/log"

	"github.com/dokzlo13/lightd/internal/config"
)

// App is the main application container that manages all services and their lifecycle.
// It provides dependency injection and enables testable architecture.
type App struct {
	cfg      *config.Config
	services *Services
	ctx      context.Context
	cancel   context.CancelFunc
}

// New creates a new App instance with all services initialized but not started.
func New(cfg *config.Config) (*App, error) {
	services, err := NewServices(cfg)
	if err != nil {
		return nil, err
	}

	return &App{
		cfg:      cfg,
		services: services,
	}, nil
}

// Start initializes and starts all services.
// The provided context is used for cancellation.
func (a *App) Start(ctx context.Context) error {
	a.ctx, a.cancel = context.WithCancel(ctx)

	// Fatal error handler - cancels the app context to trigger shutdown
	onFatalError := func(err error) {
		log.Error().Err(err).Msg("Fatal error, initiating shutdown")
		a.cancel()
	}

	if err := a.services.Start(a.ctx, onFatalError); err != nil {
		return err
	}

	log.Info().Msg("HuePlanner started")
	return nil
}

// Stop gracefully shuts down all services.
func (a *App) Stop() error {
	log.Info().Msg("Shutting down...")

	if a.cancel != nil {
		a.cancel()
	}

	if a.services != nil {
		return a.services.Stop()
	}

	return nil
}

// Wait blocks until the application context is cancelled.
func (a *App) Wait() {
	if a.ctx != nil {
		<-a.ctx.Done()
	}
}

// ClearDesiredState clears the stored desired state.
// This is useful for resetting state on startup with --reset-state flag.
func (a *App) ClearDesiredState() error {
	if a.services != nil {
		return a.services.ClearState()
	}
	return nil
}

// SignalContext creates a context that is cancelled when SIGINT or SIGTERM is received.
func SignalContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigChan
		log.Warn().Str("signal", sig.String()).Msg("Received shutdown signal")
		cancel()
	}()

	return ctx
}
