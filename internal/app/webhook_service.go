package app

import (
	"context"

	"github.com/rs/zerolog/log"

	"github.com/dokzlo13/lightd/internal/config"
	"github.com/dokzlo13/lightd/internal/eventbus"
	"github.com/dokzlo13/lightd/internal/webhook"
)

// WebhookService wraps the webhook HTTP server.
type WebhookService struct {
	cfg    *config.Config
	server *webhook.Server
}

// NewWebhookService creates a new WebhookService.
func NewWebhookService(cfg *config.Config, bus *eventbus.Bus) *WebhookService {
	server := webhook.NewServer(cfg.Webhook.Host, cfg.Webhook.Port, bus)
	return &WebhookService{
		cfg:    cfg,
		server: server,
	}
}

// Start begins the webhook server if enabled.
func (s *WebhookService) Start(ctx context.Context) {
	if !s.cfg.Webhook.Enabled {
		log.Debug().Msg("Webhook server disabled")
		return
	}

	go func() {
		if err := s.server.Run(ctx, s.cfg.ShutdownTimeout.Duration()); err != nil {
			log.Error().Err(err).Msg("Webhook server error")
		}
	}()
}
