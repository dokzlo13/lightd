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
	server := webhook.NewServer(cfg.Events.Webhook.Host, cfg.Events.Webhook.Port, bus)
	return &WebhookService{
		cfg:    cfg,
		server: server,
	}
}

// SetPathMatcher sets the path matcher for request validation.
// Should be called after Lua handlers are registered.
func (s *WebhookService) SetPathMatcher(matcher webhook.PathMatcher) {
	s.server.SetPathMatcher(matcher)
}

// Start begins the webhook server if enabled.
func (s *WebhookService) Start(ctx context.Context) {
	if !s.cfg.Events.Webhook.Enabled {
		log.Debug().Msg("Webhook server disabled")
		return
	}

	go func() {
		if err := s.server.Run(ctx, s.cfg.ShutdownTimeout.Duration()); err != nil {
			log.Error().Err(err).Msg("Webhook server error")
		}
	}()
}
