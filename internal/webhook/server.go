package webhook

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"io"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/dokzlo13/lightd/internal/events"
)

// PathMatcher checks if a request matches a registered handler
type PathMatcher interface {
	HasMatch(method, path string) bool
}

// Server is an HTTP server that receives webhooks and publishes events to the bus.
type Server struct {
	addr        string
	bus         *events.Bus
	httpServer  *http.Server
	pathMatcher PathMatcher
	mu          sync.Mutex
	done        chan struct{}
	serveErr    error
}

// NewServer creates a new webhook server.
func NewServer(host string, port int, bus *events.Bus) *Server {
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

// Start binds synchronously: failure cannot leave the application reporting ready.
func (s *Server) Start(ctx context.Context, shutdownTimeout time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.httpServer != nil {
		return fmt.Errorf("webhook already started")
	}
	listener, err := net.Listen("tcp", s.addr)
	if err != nil {
		return err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleWebhook)
	s.httpServer = &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 64 << 10}
	s.done = make(chan struct{})
	go func() {
		err := s.httpServer.Serve(listener)
		s.mu.Lock()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.serveErr = err
		}
		s.mu.Unlock()
		close(s.done)
	}()
	go func() {
		select {
		case <-ctx.Done():
			closeCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
			defer cancel()
			_ = s.Close(closeCtx)
		case <-s.done:
		}
	}()
	return nil
}
func (s *Server) Close(ctx context.Context) error {
	s.mu.Lock()
	server := s.httpServer
	done := s.done
	s.mu.Unlock()
	if server == nil {
		return nil
	}
	err := server.Shutdown(ctx)
	if err != nil {
		_ = server.Close()
	}
	select {
	case <-done:
		s.mu.Lock()
		serveErr := s.serveErr
		s.mu.Unlock()
		return errors.Join(err, serveErr)
	case <-ctx.Done():
		return errors.Join(err, ctx.Err())
	}
}
func (s *Server) Run(ctx context.Context, timeout time.Duration) error {
	if err := s.Start(ctx, timeout); err != nil {
		return err
	}
	<-s.done
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.serveErr
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
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		log.Error().Err(err).Msg("Failed to read webhook request body")
		w.Header().Set("Content-Type", "application/json")
		var sizeErr *http.MaxBytesError
		if errors.As(err, &sizeErr) {
			w.WriteHeader(http.StatusRequestEntityTooLarge)
		} else {
			w.WriteHeader(http.StatusBadRequest)
		}
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
	eventID := "webhook-" + uuid.NewString()

	log.Debug().
		Str("method", r.Method).
		Str("path", r.URL.Path).
		Int("body_len", len(body)).
		Str("event_id", eventID).
		Msg("Received webhook request")

	// Publish event to bus
	if err := s.bus.Publish(events.Event{
		Type: events.EventTypeWebhook,
		Data: map[string]interface{}{
			"method":   r.Method,
			"path":     r.URL.Path,
			"body":     string(body),
			"json":     jsonBody,
			"headers":  headers,
			"event_id": eventID,
		},
	}); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "action admission rejected", "event_id": eventID})
		return
	}

	// Accepted only after downstream admission.
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "accepted", "event_id": eventID})
}
