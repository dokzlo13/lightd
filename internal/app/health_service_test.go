package app

import (
	"context"
	"github.com/dokzlo13/lightd/internal/config"
	"net/http/httptest"
	"testing"
)

func TestLivenessIsSeparateFromReadiness(t *testing.T) {
	s := NewHealthService(&config.Config{})
	for _, tc := range []struct {
		path string
		want int
	}{{"/health", 200}, {"/ready", 503}, {"/status", 503}} {
		w := httptest.NewRecorder()
		s.handler().ServeHTTP(w, httptest.NewRequest("GET", tc.path, nil))
		if w.Code != tc.want {
			t.Fatalf("%s: %d", tc.path, w.Code)
		}
	}
	s.SetStatusProvider(func(context.Context) (bool, any) { return true, map[string]any{"bridge": "disconnected"} })
	w := httptest.NewRecorder()
	s.handler().ServeHTTP(w, httptest.NewRequest("GET", "/ready", nil))
	if w.Code != 200 {
		t.Fatal("external bridge degradation must not alone fail admission health")
	}
}
