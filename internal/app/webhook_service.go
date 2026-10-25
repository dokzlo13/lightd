package app

import (
	"context"

	"github.com/dokzlo13/lightd/internal/config"
	"github.com/dokzlo13/lightd/internal/events"
	"github.com/dokzlo13/lightd/internal/webhook"
)

// WebhookService wraps the webhook HTTP server.
type WebhookService struct {
	cfg    *config.Config
	server *webhook.Server
}

// NewWebhookService creates a new WebhookService.
func NewWebhookService(cfg *config.Config, bus *events.Bus) *WebhookService {
	server := webhook.NewServer(cfg.Events.Webhook.GetHost(), cfg.Events.Webhook.GetPort(), bus)
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

// Start binds before returning so startup cannot succeed with a broken listener.
func (s *WebhookService) Start(ctx context.Context) error {
	if !s.cfg.Events.Webhook.Enabled {
		return nil
	}
	return s.server.Start(ctx, s.cfg.GetShutdownTimeout())
}
func (s *WebhookService) Close(ctx context.Context) error { return s.server.Close(ctx) }
