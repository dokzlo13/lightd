package webhook

import (
	"context"
	"github.com/dokzlo13/lightd/internal/events"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAdmissionHTTPStatusAndBodyLimit(t *testing.T) {
	for _, reject := range []bool{false, true} {
		b := events.NewBus()
		b.SubscribeAck(events.EventTypeWebhook, func(events.Event) error {
			if reject {
				return events.ErrQueueFull
			}
			return nil
		})
		s := NewServer("127.0.0.1", 0, b)
		w := httptest.NewRecorder()
		s.handleWebhook(w, httptest.NewRequest(http.MethodPost, "/fixture", strings.NewReader(`{}`)))
		want := http.StatusAccepted
		if reject {
			want = http.StatusServiceUnavailable
		}
		if w.Code != want {
			t.Fatal(w.Code, w.Body.String())
		}
		w = httptest.NewRecorder()
		s.handleWebhook(w, httptest.NewRequest(http.MethodPost, "/fixture", strings.NewReader(strings.Repeat("x", (1<<20)+1))))
		if w.Code != http.StatusRequestEntityTooLarge {
			t.Fatal(w.Code)
		}
		b.Close(context.Background())
	}
}
func TestStartReportsBindFailure(t *testing.T) {
	ln, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer ln.Close()
	b := events.NewBus()
	defer b.Close(context.Background())
	s := NewServer("127.0.0.1", ln.Addr().(*net.TCPAddr).Port, b)
	if e = s.Start(context.Background(), time.Second); e == nil {
		t.Fatal("bind failure hidden")
	}
}
