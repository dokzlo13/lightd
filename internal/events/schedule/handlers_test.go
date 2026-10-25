package schedule

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/dokzlo13/lightd/internal/actions"
	"github.com/dokzlo13/lightd/internal/events"
	"github.com/dokzlo13/lightd/internal/storage"
	lua "github.com/yuin/gopher-lua"
)

type queueExecutor struct {
	accept bool
	work   func(context.Context)
	ctx    context.Context
}

func (q *queueExecutor) Do(ctx context.Context, work func(context.Context)) bool {
	if !q.accept {
		return false
	}
	q.work = work
	q.ctx = ctx
	return true
}

type queueReservation struct {
	q   *queueExecutor
	ctx context.Context
}

func (r *queueReservation) Commit(work func(context.Context)) error {
	r.q.work = work
	r.q.ctx = r.ctx
	return nil
}
func (r *queueReservation) Release() {}
func (q *queueExecutor) Reserve(ctx context.Context) (actions.Reservation, error) {
	if !q.accept {
		return nil, errors.New("queue full")
	}
	return &queueReservation{q, ctx}, nil
}
func (q *queueExecutor) LState() *lua.LState { return nil }
func TestAdmissionReceiptMetadataAndSupersededExecution(t *testing.T) {
	db, err := storage.Open(t.TempDir() + "/state.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ledger := storage.NewLedger(db.DB)
	registry := actions.NewRegistry()
	ran := false
	if err := registry.RegisterSimple("noop", func(*actions.Context, map[string]any) error { ran = true; return nil }); err != nil {
		t.Fatal(err)
	}
	invoker := actions.NewInvoker(registry, ledger, func(context.Context) *actions.Context { return &actions.Context{} })
	executor := &queueExecutor{}
	bus := events.NewBus()
	defer bus.Close(context.Background())
	RegisterHandler(context.Background(), bus, invoker, executor)
	at := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	valid := true
	event := events.Event{Type: events.EventTypeSchedule, Data: map[string]any{"parent_invocation_id": "parent-fixture", "action_name": "noop", "schedule_id": "phase", "occurrence_id": "key", "source": "boot_recovery", "run_at": at, "policy_version": "winter-1", "generation": int64(7), "generation_valid": func() bool { return valid }}}
	if err := bus.Publish(event); !errors.Is(err, actions.ErrAdmissionRejected) {
		t.Fatalf("rejection=%v", err)
	}
	executor.accept = true
	if err := bus.Publish(event); err != nil {
		t.Fatal(err)
	}
	var metadata string
	if err := db.DB.QueryRow(`SELECT metadata FROM invocations WHERE status='accepted'`).Scan(&metadata); err != nil {
		t.Fatal(err)
	}
	var receipt storage.Invocation
	if err := json.Unmarshal([]byte(metadata), &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt.ParentID != "parent-fixture" || receipt.Source != "boot_recovery" || !receipt.PlannedAt.Equal(at) || receipt.PolicyVersion != "winter-1" || receipt.Generation != 7 {
		t.Fatalf("lost provenance: %+v", receipt)
	}
	valid = false
	executor.work(executor.ctx)
	if ran {
		t.Fatal("superseded action executed")
	}
	var status string
	if err := db.DB.QueryRow(`SELECT status FROM invocations WHERE id=?`, receipt.ID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "failed" {
		t.Fatalf("superseded terminal=%q", status)
	}
}
