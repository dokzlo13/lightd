package app

import (
	"context"
	"fmt"
	"net/http"

	"github.com/rs/zerolog/log"

	"github.com/dokzlo13/lightd/internal/config"
)

// HealthService provides HTTP health check endpoints.
type HealthService struct {
	cfg    *config.Config
	server *http.Server
}

// NewHealthService creates a new HealthService.
func NewHealthService(cfg *config.Config) *HealthService {
	return &HealthService{
		cfg: cfg,
	}
}

// Start begins the health check server if enabled.
func (s *HealthService) Start(ctx context.Context) {
	if !s.cfg.Healthcheck.Enabled {
		return
	}

	go s.run(ctx)
}

func (s *HealthService) run(ctx context.Context) {
	addr := fmt.Sprintf("%s:%d", s.cfg.Healthcheck.GetHost(), s.cfg.Healthcheck.GetPort())

	mux := http.NewServeMux()

	// Health check endpoint
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"healthy"}`))
	})

	// Ready check endpoint
	mux.HandleFunc("/ready", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ready"}`))
	})

	s.server = &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	log.Info().Str("addr", addr).Msg("Starting health check server")

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), s.cfg.GetShutdownTimeout())
		defer cancel()
		if err := s.server.Shutdown(shutdownCtx); err != nil {
			log.Error().Err(err).Msg("Health check server shutdown error")
		}
	}()

	if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Error().Err(err).Msg("Health check server error")
	}
}
