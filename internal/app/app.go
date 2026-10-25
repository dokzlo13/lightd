package app

import (
	"context"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/dokzlo13/lightd/internal/config"
	"github.com/rs/zerolog/log"
)

type App struct {
	cfg      *config.Config
	services *Services
	ctx      context.Context
	cancel   context.CancelFunc
	mu       sync.Mutex
	fatal    error
}

func New(cfg *config.Config) (*App, error) {
	s, err := NewServices(cfg)
	if err != nil {
		return nil, err
	}
	return &App{cfg: cfg, services: s}, nil
}
func (a *App) Start(ctx context.Context) error {
	a.ctx, a.cancel = context.WithCancel(ctx)
	onFatal := func(err error) {
		a.mu.Lock()
		if a.fatal == nil {
			a.fatal = err
		}
		a.mu.Unlock()
		log.Error().Err(err).Msg("Fatal service error")
		a.cancel()
	}
	if err := a.services.Start(a.ctx, onFatal); err != nil {
		a.cancel()
		_ = a.services.Stop()
		return err
	}
	log.Info().Msg("lightd started")
	return nil
}
func (a *App) Stop() error {
	if a.cancel != nil {
		a.cancel()
	}
	if a.services != nil {
		return a.services.Stop()
	}
	return nil
}
func (a *App) Wait() error {
	if a.ctx != nil {
		<-a.ctx.Done()
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.fatal
}
func (a *App) ClearDesiredState() error {
	if a.services != nil {
		return a.services.ClearState()
	}
	return nil
}
func SignalContext() context.Context {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	go func() { <-ctx.Done(); stop() }()
	return ctx
}
