package storage

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func testLedger(t *testing.T) (*Ledger, *DB) {
	t.Helper()
	db, e := Open(filepath.Join(t.TempDir(), "test.db"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { db.Close() })
	return NewLedger(db.DB), db
}
func TestRecoveryDistinguishesUnstartedAndUnknown(t *testing.T) {
	l, db := testLedger(t)
	ctx := context.Background()
	a := Invocation{ID: "a", Action: "test", Key: "a", Source: "test"}
	b := Invocation{ID: "b", Action: "test", Key: "b", Source: "test"}
	for _, in := range []Invocation{a, b} {
		if ok, e := l.Receive(ctx, in); !ok || e != nil {
			t.Fatal(ok, e)
		}
	}
	if e := l.StartInvocation(ctx, b); e != nil {
		t.Fatal(e)
	}
	if e := l.RecoverInterrupted(); e != nil {
		t.Fatal(e)
	}
	for id, want := range map[string]string{"a": "interrupted_before_start", "b": "interrupted_outcome_unknown"} {
		var status string
		if e := db.QueryRow(`SELECT status FROM invocations WHERE id=?`, id).Scan(&status); e != nil {
			t.Fatal(e)
		}
		if status != want {
			t.Fatalf("%s: %s", id, status)
		}
	}
}
func TestHistoryDisabledKeepsDedupe(t *testing.T) {
	l, db := testLedger(t)
	l.SetHistoryEnabled(false)
	in := Invocation{ID: "a", Action: "test", Key: "key", Source: "test"}
	ok, e := l.Receive(context.Background(), in)
	if !ok || e != nil {
		t.Fatal(ok, e)
	}
	if e = l.FinishInvocation(context.Background(), in, "completed", nil, nil); e != nil {
		t.Fatal(e)
	}
	in.ID = "b"
	ok, e = l.Receive(context.Background(), in)
	if ok || e != nil {
		t.Fatal(ok, e)
	}
	var n int
	db.QueryRow(`SELECT count(*) FROM event_ledger`).Scan(&n)
	if n != 0 {
		t.Fatal("optional history recorded")
	}
}
func TestRetentionPreservesMinimumAndUnknown(t *testing.T) {
	l, db := testLedger(t)
	ctx := context.Background()
	for _, id := range []string{"recent", "old", "uncertain"} {
		in := Invocation{ID: id, Action: "test", Key: id, Source: "test"}
		l.Receive(ctx, in)
		status := "completed"
		if id == "uncertain" {
			status = "interrupted_outcome_unknown"
		}
		l.FinishInvocation(ctx, in, status, nil, nil)
		age := 8 * 24 * time.Hour
		if id == "recent" {
			age = 4 * 24 * time.Hour
		}
		db.Exec(`UPDATE invocations SET received_at=? WHERE id=?`, time.Now().Add(-age).UnixNano(), id)
	}
	if _, e := l.DeleteOlderThan(time.Hour); e != nil {
		t.Fatal(e)
	}
	var n int
	db.QueryRow(`SELECT count(*) FROM invocations`).Scan(&n)
	if n != 2 {
		t.Fatalf("retained %d", n)
	}
}
func TestCursorMonotonicAndIndependent(t *testing.T) {
	l, _ := testLedger(t)
	now := time.Now()
	if e := l.SaveCursor("schedule:g1", now); e != nil {
		t.Fatal(e)
	}
	l.SaveCursor("schedule:g1", now.Add(-time.Hour))
	got, ok, e := l.LoadCursor("schedule:g1")
	if !ok || e != nil || !got.Equal(now) {
		t.Fatal(got, ok, e)
	}
}

func TestIngressReceiptsSurviveCollectionAndLinkOutcomes(t *testing.T) {
	l, db := testLedger(t)
	ctx := context.Background()
	for _, id := range []string{"pending", "linked"} {
		ok, e := l.ReceiveEvent(ctx, id, "button")
		if !ok || e != nil {
			t.Fatal(ok, e)
		}
	}
	if ok, e := l.ReceiveEvent(ctx, "pending", "button"); ok || e != nil {
		t.Fatal("duplicate input accepted", ok, e)
	}
	in := Invocation{ID: "batch", Action: "test", Source: "button", EventIDs: []string{"linked"}}
	if ok, e := l.Receive(ctx, in); !ok || e != nil {
		t.Fatal(ok, e)
	}
	if e := l.StartInvocation(ctx, in); e != nil {
		t.Fatal(e)
	}
	if e := l.RecoverInterrupted(); e != nil {
		t.Fatal(e)
	}
	for id, want := range map[string]string{"pending": "interrupted_before_dispatch", "linked": "interrupted_outcome_unknown"} {
		var status string
		db.QueryRow(`SELECT status FROM ingress_receipts WHERE id=?`, id).Scan(&status)
		if status != want {
			t.Fatalf("%s got %s want %s", id, status, want)
		}
	}
}
