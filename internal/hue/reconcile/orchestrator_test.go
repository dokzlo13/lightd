package reconcile

import (
	"context"
	"database/sql"
	"errors"
	_ "github.com/mattn/go-sqlite3"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type fakeResource struct {
	mu       sync.Mutex
	version  int64
	attempts int
	fail     bool
}

func (r *fakeResource) Key() ResourceKey           { return ResourceKey{KindGroup, "fixture"} }
func (r *fakeResource) Load(context.Context) error { return nil }
func (r *fakeResource) NeedsReconcile() bool       { return true }
func (r *fakeResource) ReconcileStep(context.Context) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.attempts++
	if r.fail {
		return false, errors.New("temporary bridge failure")
	}
	return true, nil
}
func (r *fakeResource) DesiredVersion() int64 { return r.version }

type fakeProvider struct{ r *fakeResource }

func (p fakeProvider) Kind() Kind { return KindGroup }
func (p fakeProvider) ListDirty(_ context.Context, last map[string]int64) ([]Resource, error) {
	if last["fixture"] < p.r.version {
		return []Resource{p.r}, nil
	}
	return nil, nil
}
func (p fakeProvider) ListAllIDs(context.Context) ([]string, error)  { return []string{"fixture"}, nil }
func (p fakeProvider) Get(context.Context, string) (Resource, error) { return p.r, nil }
func (p fakeProvider) ClearCaches()                                  {}
func TestFailedForcedIntentRemainsPendingWithBackoff(t *testing.T) {
	r := &fakeResource{version: 1}
	o := NewOrchestrator(0, 0, 10)
	o.Register(fakeProvider{r})
	ctx := context.Background()
	o.reconcileAll(ctx)
	if r.attempts != 1 {
		t.Fatal(r.attempts)
	}
	r.fail = true
	o.TriggerResource(r.Key())
	o.reconcileAll(ctx)
	if r.attempts != 2 {
		t.Fatal(r.attempts)
	}
	r.fail = false
	o.reconcileAll(ctx)
	if r.attempts != 2 {
		t.Fatal("backoff ignored")
	}
	o.mu.Lock()
	retry := o.retries[r.Key()]
	retry.Next = time.Now().Add(-time.Second)
	o.retries[r.Key()] = retry
	o.mu.Unlock()
	o.reconcileAll(ctx)
	if r.attempts != 3 || len(o.Status().Retrying) != 0 {
		t.Fatalf("forced intent did not recover %#v", o.Status())
	}
}
func TestNewDesiredVersionBypassesOldFailureBackoff(t *testing.T) {
	r := &fakeResource{version: 1, fail: true}
	o := NewOrchestrator(0, 0, 10)
	o.Register(fakeProvider{r})
	o.reconcileAll(context.Background())
	r.version = 2
	r.fail = false
	o.reconcileAll(context.Background())
	if r.attempts != 2 {
		t.Fatal("new desired version was delayed by old retry")
	}
}
func TestRetryLoopOperatesWithPeriodicReconciliationDisabled(t *testing.T) {
	r := &fakeResource{version: 1, fail: true}
	o := NewOrchestrator(0, 0, 10)
	o.Register(fakeProvider{r})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = o.Run(ctx); close(done) }()
	defer func() { cancel(); <-done }()
	deadline := time.After(3 * time.Second)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-deadline:
			t.Fatal("retry never ran")
		case <-ticker.C:
			r.mu.Lock()
			n := r.attempts
			r.mu.Unlock()
			if n >= 2 {
				return
			}
		}
	}
}

func deliveryDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "delivery.db"))
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	return db
}
func TestAcknowledgedDeliverySurvivesRestartAndForceReasserts(t *testing.T) {
	db := deliveryDB(t)
	r := &fakeResource{version: 1}
	o := NewOrchestrator(0, 0, 10)
	o.Register(fakeProvider{r})
	if err := o.SetDB(db); err != nil {
		t.Fatal(err)
	}
	o.reconcileAll(context.Background())
	if r.attempts != 1 {
		t.Fatal(r.attempts)
	}
	restarted := NewOrchestrator(0, 0, 10)
	restarted.Register(fakeProvider{r})
	if err := restarted.SetDB(db); err != nil {
		t.Fatal(err)
	}
	restarted.reconcileAll(context.Background())
	if r.attempts != 1 {
		t.Fatal("acknowledged old desired replayed on boot")
	}
	restarted.TriggerResource(r.Key())
	restarted.reconcileAll(context.Background())
	if r.attempts != 2 {
		t.Fatal("explicit force did not reassert")
	}
	third := NewOrchestrator(0, 0, 10)
	third.Register(fakeProvider{r})
	if err := third.SetDB(db); err != nil {
		t.Fatal(err)
	}
	third.reconcileAll(context.Background())
	if r.attempts != 2 {
		t.Fatal("forced acknowledgment did not persist")
	}
}
func TestRetryAgeSurvivesRestartAndExpires(t *testing.T) {
	db := deliveryDB(t)
	r := &fakeResource{version: 1, fail: true}
	o := NewOrchestrator(0, 0, 10)
	o.Register(fakeProvider{r})
	if err := o.SetDB(db); err != nil {
		t.Fatal(err)
	}
	o.reconcileAll(context.Background())
	if _, err := db.Exec(`UPDATE hue_delivery_v1 SET first_attempt=?,next_attempt=?`, time.Now().Add(-11*time.Minute).UnixMilli(), time.Now().Add(-time.Second).UnixMilli()); err != nil {
		t.Fatal(err)
	}
	restarted := NewOrchestrator(0, 0, 10)
	restarted.Register(fakeProvider{r})
	if err := restarted.SetDB(db); err != nil {
		t.Fatal(err)
	}
	restarted.reconcileAll(context.Background())
	if r.attempts != 1 || !restarted.Status().Retrying["group:fixture"].Expired {
		t.Fatal("old failure age was reset across restart")
	}
	r.version = 2
	r.fail = false
	restarted.reconcileAll(context.Background())
	if r.attempts != 2 {
		t.Fatal("new desired version failed to recover expired work")
	}
}

func TestImportedHistoricalDesiredIsNotReplayed(t *testing.T) {
	db := deliveryDB(t)
	if _, err := db.Exec(`CREATE TABLE resource_state(kind TEXT,id TEXT,version INTEGER); INSERT INTO resource_state VALUES('group','fixture',1)`); err != nil {
		t.Fatal(err)
	}
	r := &fakeResource{version: 1}
	o := NewOrchestrator(0, 0, 10)
	o.Register(fakeProvider{r})
	if err := o.SetDB(db); err != nil {
		t.Fatal(err)
	}
	o.reconcileAll(context.Background())
	if r.attempts != 0 {
		t.Fatal("unknown historical desired caused a startup write")
	}
	s := o.Status()
	if s.Imported["group:fixture"] != 1 || len(s.Acknowledged) != 0 {
		t.Fatalf("import confused with acknowledgment: %#v", s)
	}
	r.version = 2
	o.reconcileAll(context.Background())
	if r.attempts != 1 {
		t.Fatal("fresh desired frame did not activate")
	}
}
