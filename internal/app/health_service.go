package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/dokzlo13/lightd/internal/buildinfo"
	"github.com/dokzlo13/lightd/internal/config"
	"github.com/rs/zerolog/log"
)

// HealthService distinguishes process liveness from application admission health.
type HealthService struct {
	cfg      *config.Config
	mu       sync.RWMutex
	server   *http.Server
	done     chan struct{}
	provider func(context.Context) (bool, any)
}

func NewHealthService(cfg *config.Config) *HealthService { return &HealthService{cfg: cfg} }
func (s *HealthService) SetStatusProvider(fn func(context.Context) (bool, any)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.provider = fn
}
func (s *HealthService) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"status": "alive", "build": buildinfo.Info()})
	})
	status := func(w http.ResponseWriter, r *http.Request) {
		s.mu.RLock()
		fn := s.provider
		s.mu.RUnlock()
		ready := false
		var detail any
		if fn != nil {
			ctx, cancel := context.WithTimeout(r.Context(), time.Second)
			defer cancel()
			ready, detail = fn(ctx)
		}
		code := http.StatusServiceUnavailable
		if ready {
			code = http.StatusOK
		}
		writeJSON(w, code, map[string]any{"ready": ready, "build": buildinfo.Info(), "details": detail})
	}
	mux.HandleFunc("/ready", status)
	mux.HandleFunc("/status", status)
	return mux
}
func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// Start binds synchronously: address conflicts fail application startup.
func (s *HealthService) Start(ctx context.Context) error {
	if !s.cfg.Healthcheck.Enabled {
		return nil
	}
	ln, err := net.Listen("tcp", net.JoinHostPort(s.cfg.Healthcheck.GetHost(), fmt.Sprint(s.cfg.Healthcheck.GetPort())))
	if err != nil {
		return fmt.Errorf("health listener: %w", err)
	}
	s.mu.Lock()
	s.server = &http.Server{Handler: s.handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second}
	server := s.server
	s.done = make(chan struct{})
	done := s.done
	s.mu.Unlock()
	go func() {
		defer close(done)
		if err := server.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error().Err(err).Msg("Health server stopped unexpectedly")
		}
	}()
	go func() {
		select {
		case <-ctx.Done():
			shutdown, cancel := context.WithTimeout(context.Background(), s.cfg.GetShutdownTimeout())
			defer cancel()
			_ = s.Close(shutdown)
		case <-done:
		}
	}()
	return nil
}
func (s *HealthService) Close(ctx context.Context) error {
	s.mu.RLock()
	server, done := s.server, s.done
	s.mu.RUnlock()
	if server == nil {
		return nil
	}
	if err := server.Shutdown(ctx); err != nil {
		_ = server.Close()
		return err
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
