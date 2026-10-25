package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Invocation records acceptance independently of optional deduplication.
// A failed/interrupted invocation remains claimed: imperative actions are never
// automatically repeated after an ambiguous external effect.
type Invocation struct {
	ID            string    `json:"id"`
	Action        string    `json:"action"`
	Key           string    `json:"dedupe_key,omitempty"`
	Source        string    `json:"source"`
	Definition    string    `json:"definition,omitempty"`
	ParentID      string    `json:"parent_id,omitempty"`
	PlannedAt     time.Time `json:"planned_at,omitempty"`
	PolicyVersion string    `json:"policy_version,omitempty"`
	Generation    int64     `json:"generation,omitempty"`
}

func initInvocations(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS invocations (
 id TEXT PRIMARY KEY, action TEXT NOT NULL, dedupe_key TEXT NOT NULL DEFAULT '',
 source TEXT NOT NULL, definition TEXT NOT NULL DEFAULT '', metadata TEXT NOT NULL,
 status TEXT NOT NULL, received_at INTEGER NOT NULL, started_at INTEGER, finished_at INTEGER, error TEXT NOT NULL DEFAULT '');
 CREATE UNIQUE INDEX IF NOT EXISTS idx_invocation_claim ON invocations(dedupe_key) WHERE dedupe_key != '' AND status != 'rejected';
 CREATE INDEX IF NOT EXISTS idx_invocation_received ON invocations(received_at);
 CREATE TABLE IF NOT EXISTS scheduler_cursors(id TEXT PRIMARY KEY,planned_at INTEGER NOT NULL);`)
	return err
}

func (l *Ledger) HasCompletedE(key string) (bool, error) {
	if key == "" {
		return false, nil
	}
	var one int
	err := l.db.QueryRow(`SELECT 1 FROM event_ledger WHERE idempotency_key=? AND event_type=? LIMIT 1`, key, string(EventActionCompleted)).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

// Receive commits the receipt before admitting work. false means a prior claim
// already exists, including a completed claim from the previous schema.
func (l *Ledger) Receive(ctx context.Context, in Invocation) (bool, error) {
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	if in.Key != "" {
		var count int
		err = tx.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM invocations WHERE dedupe_key=? AND status!='rejected') + (SELECT count(*) FROM event_ledger WHERE idempotency_key=? AND event_type='action_completed')`, in.Key, in.Key).Scan(&count)
		if err != nil {
			return false, err
		}
		if count > 0 {
			return false, nil
		}
	}
	metadata, err := json.Marshal(in)
	if err != nil {
		return false, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO invocations(id,action,dedupe_key,source,definition,metadata,status,received_at) VALUES(?,?,?,?,?,?,'accepted',?)`, in.ID, in.Action, in.Key, in.Source, in.Definition, string(metadata), time.Now().UnixNano())
	if err != nil {
		return false, err
	}
	if err = appendInvocationTx(tx, in, "action_received", ""); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

func (l *Ledger) StartInvocation(ctx context.Context, in Invocation) error {
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	r, err := tx.ExecContext(ctx, `UPDATE invocations SET status='running',started_at=? WHERE id=? AND status='accepted'`, time.Now().UnixNano(), in.ID)
	if err != nil {
		return err
	}
	n, _ := r.RowsAffected()
	if n != 1 {
		return fmt.Errorf("invocation %s is not accepted", in.ID)
	}
	if err = appendInvocationTx(tx, in, "action_started", ""); err != nil {
		return err
	}
	return tx.Commit()
}

// FinishInvocation makes desired writes and the successful outcome one commit.
// Failed/rejected outcomes never execute commit. Use a fresh bounded context to
// record cancellation, because the action context may already be cancelled.
func (l *Ledger) FinishInvocation(ctx context.Context, in Invocation, status string, actionErr error, commit func(*sql.Tx) error) error {
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	message := ""
	if actionErr != nil {
		message = actionErr.Error()
	}
	if status == "completed" && commit != nil {
		if err = commit(tx); err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, `UPDATE invocations SET status=?,finished_at=?,error=? WHERE id=?`, status, time.Now().UnixNano(), message, in.ID)
	if err != nil {
		return err
	}
	if err = appendInvocationTx(tx, in, "action_"+status, message); err != nil {
		return err
	}
	return tx.Commit()
}
func appendInvocationTx(tx *sql.Tx, in Invocation, event, message string) error {
	payload := map[string]any{"invocation_id": in.ID, "action": in.Action, "parent_id": in.ParentID, "planned_at": in.PlannedAt, "policy_version": in.PolicyVersion, "generation": in.Generation}
	if message != "" {
		payload["error"] = message
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`INSERT INTO event_ledger(event_type,timestamp,payload,source,idempotency_key,def_id) VALUES(?,?,?,?,?,?)`, event, time.Now().Unix(), string(b), in.Source, in.Key, in.Definition)
	return err
}

// RecoverInterrupted closes receipts left by a previous process; it never replays
// effects whose completion cannot be established.
func (l *Ledger) RecoverInterrupted() error {
	rows, err := l.db.Query(`SELECT metadata FROM invocations WHERE status IN ('accepted','running')`)
	if err != nil {
		return err
	}
	var pending []Invocation
	for rows.Next() {
		var b string
		if err = rows.Scan(&b); err != nil {
			rows.Close()
			return err
		}
		var in Invocation
		if err = json.Unmarshal([]byte(b), &in); err != nil {
			rows.Close()
			return err
		}
		pending = append(pending, in)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, in := range pending {
		if err = l.FinishInvocation(context.Background(), in, "interrupted", errors.New("previous process stopped before terminal outcome"), nil); err != nil {
			return err
		}
	}
	return nil
}

func (l *Ledger) RunRetention(ctx context.Context, retention, interval time.Duration) {
	if retention <= 0 || interval <= 0 {
		return
	}
	clean := func() { _, _ = l.DeleteOlderThan(retention) }
	clean()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			clean()
		}
	}
}

func (s *Store) Transaction(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) SetTx(tx *sql.Tx, kind, id string, payload []byte) error {
	_, err := tx.Exec(`INSERT INTO resource_state(kind,id,payload,version,updated_at) VALUES(?,?,?,1,?) ON CONFLICT(kind,id) DO UPDATE SET payload=excluded.payload,version=version+1,updated_at=excluded.updated_at`, kind, id, string(payload), time.Now().Unix())
	return err
}
func (s *TypedStore[T]) UpdateTx(tx *sql.Tx, id string, modify func(T) T) error {
	var current T
	var payload string
	err := tx.QueryRow(`SELECT payload FROM resource_state WHERE kind=? AND id=?`, s.kind, id).Scan(&payload)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err == nil {
		if err = json.Unmarshal([]byte(payload), &current); err != nil {
			return err
		}
	}
	b, err := json.Marshal(modify(current))
	if err != nil {
		return err
	}
	return s.store.SetTx(tx, s.kind, id, b)
}
func (s *TypedStore[T]) Transaction(ctx context.Context, fn func(*sql.Tx) error) error {
	return s.store.Transaction(ctx, fn)
}

func (l *Ledger) LoadCursor(id string) (time.Time, bool, error) {
	var n int64
	err := l.db.QueryRow(`SELECT planned_at FROM scheduler_cursors WHERE id=?`, id).Scan(&n)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, err
	}
	return time.Unix(0, n), true, nil
}
func (l *Ledger) SaveCursor(id string, at time.Time) error {
	_, err := l.db.Exec(`INSERT INTO scheduler_cursors(id,planned_at) VALUES(?,?) ON CONFLICT(id) DO UPDATE SET planned_at=max(planned_at,excluded.planned_at)`, id, at.UnixNano())
	return err
}
