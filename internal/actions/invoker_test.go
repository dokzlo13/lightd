package actions

import (
	"context"
	"database/sql"
	"errors"
	"github.com/dokzlo13/lightd/internal/storage"
	"path/filepath"
	"testing"
	"time"
)

func setupInvoker(t *testing.T) (*Invoker, *Registry, *storage.DB) {
	t.Helper()
	db, e := storage.Open(filepath.Join(t.TempDir(), "test.db"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { db.Close() })
	r := NewRegistry()
	return NewInvoker(r, storage.NewLedger(db.DB), func(ctx context.Context) *Context { return NewContext(ctx, nil, nil, nil, nil) }), r, db
}
func queryCount(t *testing.T, db *sql.DB, q string) int {
	t.Helper()
	var n int
	if e := db.QueryRow(q).Scan(&n); e != nil {
		t.Fatal(e)
	}
	return n
}
func TestAtomicDesiredAndCompletion(t *testing.T) {
	for _, failure := range []string{"none", "desired", "outcome", "receipt", "start"} {
		t.Run(failure, func(t *testing.T) {
			i, r, db := setupInvoker(t)
			store := storage.NewTypedStore[map[string]any](storage.NewStore(db.DB), "light")
			calls := 0
			r.RegisterSimple("test", func(c *Context, _ map[string]any) error {
				calls++
				c.DeferCommit(func(tx *sql.Tx) error {
					return store.UpdateTx(tx, "fake", func(m map[string]any) map[string]any { return map[string]any{"brightness": 42} })
				}, nil)
				return nil
			})
			var trigger string
			switch failure {
			case "desired":
				trigger = `CREATE TRIGGER fail BEFORE INSERT ON resource_state BEGIN SELECT RAISE(ABORT,'fault'); END;`
			case "outcome":
				trigger = `CREATE TRIGGER fail BEFORE INSERT ON event_ledger WHEN NEW.event_type='action_completed' BEGIN SELECT RAISE(ABORT,'fault'); END;`
			case "receipt":
				trigger = `CREATE TRIGGER fail BEFORE INSERT ON invocations BEGIN SELECT RAISE(ABORT,'fault'); END;`
			case "start":
				trigger = `CREATE TRIGGER fail BEFORE INSERT ON event_ledger WHEN NEW.event_type='action_started' BEGIN SELECT RAISE(ABORT,'fault'); END;`
			}
			if trigger != "" {
				if _, e := db.Exec(trigger); e != nil {
					t.Fatal(e)
				}
			}
			err := i.Invoke(context.Background(), "test", nil, "key")
			if failure == "none" {
				if err != nil {
					t.Fatal(err)
				}
				if queryCount(t, db.DB, `SELECT count(*) FROM resource_state`) != 1 {
					t.Fatal("missing desired")
				}
				if queryCount(t, db.DB, `SELECT count(*) FROM invocations WHERE status='completed'`) != 1 {
					t.Fatal("missing completion")
				}
			} else {
				if err == nil {
					t.Fatal("fault reported success")
				}
				if queryCount(t, db.DB, `SELECT count(*) FROM resource_state`) != 0 {
					t.Fatal("partial desired committed")
				}
				if queryCount(t, db.DB, `SELECT count(*) FROM invocations WHERE status='completed'`) != 0 {
					t.Fatal("false completed")
				}
				if (failure == "receipt" || failure == "start") && calls != 0 {
					t.Fatal("executed without receipt/start")
				}
			}
		})
	}
}

type queueExecutor struct {
	accept bool
	work   func(context.Context)
}

func (e *queueExecutor) Do(_ context.Context, f func(context.Context)) bool {
	if !e.accept {
		return false
	}
	e.work = f
	return true
}
func TestRejectedAdmissionCanRetryButFailedActionCannot(t *testing.T) {
	i, r, db := setupInvoker(t)
	calls := 0
	r.RegisterSimple("test", func(*Context, map[string]any) error { calls++; return errors.New("failed") })
	x := &queueExecutor{}
	if !errors.Is(i.Queue(context.Background(), x, "test", nil, "key", "button", ""), ErrAdmissionRejected) {
		t.Fatal("wanted rejection")
	}
	x.accept = true
	if e := i.Queue(context.Background(), x, "test", nil, "key", "button", ""); e != nil {
		t.Fatal(e)
	}
	x.work(context.Background())
	if e := i.Invoke(context.Background(), "test", nil, "key"); e != nil {
		t.Fatal(e)
	}
	if calls != 1 {
		t.Fatalf("duplicate side effect: %d", calls)
	}
	if queryCount(t, db.DB, `SELECT count(*) FROM invocations WHERE status='accepted'`) != 0 || queryCount(t, db.DB, `SELECT count(*) FROM invocations WHERE status='failed'`) != 1 {
		t.Fatal("missing terminal outcomes")
	}
}
func TestManualActionsAuditedAndPanicTerminal(t *testing.T) {
	i, r, db := setupInvoker(t)
	r.RegisterSimple("panic", func(*Context, map[string]any) error { panic("fixture") })
	for j := 0; j < 2; j++ {
		if i.Invoke(context.Background(), "panic", nil, "") == nil {
			t.Fatal("panic returned success")
		}
	}
	if queryCount(t, db.DB, `SELECT count(*) FROM invocations WHERE status='failed'`) != 2 {
		t.Fatal("unkeyed outcomes missing")
	}
}
func TestPreparationFailureDoesNotExecute(t *testing.T) {
	i, r, _ := setupInvoker(t)
	calls := 0
	r.RegisterSimple("test", func(*Context, map[string]any) error { calls++; return nil })
	x := &queueExecutor{accept: true}
	if e := i.QueuePrepared(context.Background(), x, "test", "id", "button", "", func(context.Context) (map[string]any, error) { return nil, errors.New("bad reducer") }); e != nil {
		t.Fatal(e)
	}
	x.work(context.Background())
	if calls != 0 {
		t.Fatal("action ran despite failed reducer")
	}
}

func TestCommitGateAcquiredBeforeTransactionAndReleasedOnFailure(t *testing.T) {
	i, r, db := setupInvoker(t)
	held := false
	released := false
	r.RegisterSimple("guarded", func(c *Context, _ map[string]any) error {
		c.SetCommitGuard(func(ctx context.Context) (func(), error) {
			// Single-connection DB can only answer here if no SQL transaction started.
			var n int
			check, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
			defer cancel()
			if e := db.QueryRowContext(check, `SELECT 1`).Scan(&n); e != nil {
				return nil, e
			}
			held = true
			return func() { held = false; released = true }, nil
		})
		c.DeferCommit(func(tx *sql.Tx) error {
			if !held {
				t.Error("transaction outside gate")
			}
			return errors.New("injected commit failure")
		}, nil)
		return nil
	})
	if i.Invoke(context.Background(), "guarded", nil, "") == nil {
		t.Fatal("expected commit failure")
	}
	if held || !released {
		t.Fatal("gate leaked")
	}
}

func (e *queueExecutor) Reserve(ctx context.Context) (Reservation, error) {
	if !e.accept {
		return nil, ErrAdmissionRejected
	}
	return &fakeReservation{executor: e}, nil
}

type fakeReservation struct{ executor *queueExecutor }

func (r *fakeReservation) Commit(fn func(context.Context)) error { r.executor.work = fn; return nil }
func (r *fakeReservation) Release()                              {}

func TestReserveFirstAvoidsOrphanWhenRejectionStorageFails(t *testing.T) {
	i, r, db := setupInvoker(t)
	calls := 0
	r.RegisterSimple("test", func(*Context, map[string]any) error { calls++; return nil })
	// The old receipt-before-Do sequence orphaned an accepted row when rejection
	// persistence failed. Saturation now returns before any receipt transaction.
	if _, e := db.Exec(`CREATE TRIGGER reject_outcome BEFORE INSERT ON event_ledger WHEN NEW.event_type='action_rejected' BEGIN SELECT RAISE(ABORT,'fault'); END;`); e != nil {
		t.Fatal(e)
	}
	x := &queueExecutor{accept: false}
	if !errors.Is(i.Queue(context.Background(), x, "test", nil, "key", "schedule", "fixture"), ErrAdmissionRejected) {
		t.Fatal("rejection expected")
	}
	if n := queryCount(t, db.DB, `SELECT count(*) FROM invocations`); n != 0 {
		t.Fatalf("orphan receipt %d", n)
	}
	x.accept = true
	if e := i.Queue(context.Background(), x, "test", nil, "key", "schedule", "fixture"); e != nil {
		t.Fatal(e)
	}
	x.work(context.Background())
	if calls != 1 {
		t.Fatalf("lost occurrence %d", calls)
	}
}
