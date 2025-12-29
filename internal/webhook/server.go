package webhook

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/dokzlo13/lightd/internal/eventbus"
)

// PathMatcher checks if a request matches a registered handler
type PathMatcher interface {
	HasMatch(method, path string) bool
}

// Server is an HTTP server that receives webhooks and publishes events to the bus.
type Server struct {
	addr        string
	bus         *eventbus.Bus
	httpServer  *http.Server
	pathMatcher PathMatcher
}

// NewServer creates a new webhook server.
func NewServer(host string, port int, bus *eventbus.Bus) *Server {
	return &Server{
		addr: fmt.Sprintf("%s:%d", host, port),
		bus:  bus,
	}
}

// SetPathMatcher sets the path matcher for request validation.
// Must be called before Run() or after Lua handlers are registered.
func (s *Server) SetPathMatcher(matcher PathMatcher) {
	s.pathMatcher = matcher
}

// Run starts the webhook server. It blocks until the context is cancelled.
func (s *Server) Run(ctx context.Context, shutdownTimeout time.Duration) error {
	mux := http.NewServeMux()

	// Catch-all handler for all webhook requests
	mux.HandleFunc("/", s.handleWebhook)

	s.httpServer = &http.Server{
		Addr:    s.addr,
		Handler: mux,
	}

	log.Info().Str("addr", s.addr).Msg("Starting webhook server")

	// Handle graceful shutdown
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := s.httpServer.Shutdown(shutdownCtx); err != nil {
			log.Error().Err(err).Msg("Webhook server shutdown error")
		}
	}()

	if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}

	return nil
}

// handleWebhook processes incoming webhook requests and publishes them to the event bus.
func (s *Server) handleWebhook(w http.ResponseWriter, r *http.Request) {
	// Validate path if matcher is set
	if s.pathMatcher != nil && !s.pathMatcher.HasMatch(r.Method, r.URL.Path) {
		log.Debug().
			Str("method", r.Method).
			Str("path", r.URL.Path).
			Msg("No handler registered for webhook path")

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error":"no matching handler for path"}`))
		return
	}

	// Read request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		log.Error().Err(err).Msg("Failed to read webhook request body")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"failed to read request body"}`))
		return
	}
	defer r.Body.Close()

	// Try to parse body as JSON
	var jsonBody map[string]interface{}
	if len(body) > 0 {
		if err := json.Unmarshal(body, &jsonBody); err != nil {
			// Not valid JSON, that's fine - jsonBody will be nil
			jsonBody = nil
		}
	}

	// Build headers map
	headers := make(map[string]interface{})
	for key, values := range r.Header {
		if len(values) == 1 {
			headers[key] = values[0]
		} else {
			headers[key] = values
		}
	}

	// Generate unique event ID
	eventID := fmt.Sprintf("webhook-%s-%d", r.URL.Path, time.Now().UnixNano())

	log.Debug().
		Str("method", r.Method).
		Str("path", r.URL.Path).
		Int("body_len", len(body)).
		Str("event_id", eventID).
		Msg("Received webhook request")

	// Publish event to bus
	s.bus.Publish(eventbus.Event{
		Type: eventbus.EventTypeWebhook,
		Data: map[string]interface{}{
			"method":   r.Method,
			"path":     r.URL.Path,
			"body":     string(body),
			"json":     jsonBody,
			"headers":  headers,
			"event_id": eventID,
		},
	})

	// Respond with 200 OK - request accepted
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"accepted"}`))
}
