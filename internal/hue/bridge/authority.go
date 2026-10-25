package bridge

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"

	"github.com/google/uuid"
)

var ErrSuperseded = errors.New("Hue intent superseded by a newer command")

type targetAuthority struct {
	Generation   int64
	Kind, ID     string
	Version      int64
	Acknowledged bool
}

// AcquireCommand is the shared publication/final-send guard. Callers must
// acquire it BEFORE opening a desired-state SQL transaction. Reads and rate
// waits remain outside this gate; an in-flight HTTP request is bounded.
func (c *Client) AcquireCommand(ctx context.Context) (func(), error) {
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case c.commandGate <- struct{}{}:
	}
	var once sync.Once
	return func() { once.Do(func() { <-c.commandGate }) }, nil
}
func (c *Client) desiredMembers(kind, id string) ([]string, error) {
	if err := ValidateID(id); err != nil {
		return nil, err
	}
	if kind == "light" {
		return []string{id}, nil
	}
	if kind != "group" {
		return nil, fmt.Errorf("unknown desired resource kind %q", kind)
	}
	g, err := c.group(id)
	if err != nil {
		return nil, err
	}
	if len(g.Members) == 0 {
		return nil, fmt.Errorf("group has no light members")
	}
	return g.Members, nil
}

// RecordDesiredTx atomically publishes authority for every affected member in
// the same transaction as desired state and action outcome. No network or
// separate connection is used. The caller holds AcquireCommand's guard.
func (c *Client) RecordDesiredTx(tx *sql.Tx, kind, id string, version int64) error {
	ids, err := c.desiredMembers(kind, id)
	if err != nil {
		return err
	}
	for _, lightID := range ids {
		if _, err = tx.Exec(`INSERT INTO hue_target_authority_v1(light_id,generation,kind,resource_id,version,acknowledged) VALUES(?,1,?,?,?,0) ON CONFLICT(light_id) DO UPDATE SET generation=generation+1,kind=excluded.kind,resource_id=excluded.resource_id,version=excluded.version,acknowledged=0`, lightID, kind, id, version); err != nil {
			return err
		}
	}
	return nil
}

// ReassertDesired runs only for an explicit force generation, never an ordinary
// retry. The delivery journal is persisted pending before this is called.
func (c *Client) ReassertDesired(ctx context.Context, kind, id string, version int64) error {
	release, err := c.AcquireCommand(ctx)
	if err != nil {
		return err
	}
	defer release()
	if c.db == nil {
		return fmt.Errorf("durable desired authority requires database")
	}
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var current int64
	if err = tx.QueryRowContext(ctx, `SELECT version FROM resource_state WHERE kind=? AND id=?`, kind, id).Scan(&current); err != nil {
		return err
	}
	if current != version {
		return ErrSuperseded
	}
	if err = c.RecordDesiredTx(tx, kind, id, version); err != nil {
		return err
	}
	return tx.Commit()
}
func (c *Client) admitDirect(ctx context.Context, ids []string) (map[string]targetAuthority, error) {
	release, err := c.AcquireCommand(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	id := uuid.NewString()
	out := map[string]targetAuthority{}
	if c.db == nil {
		for _, lightID := range ids {
			v := c.memoryAuthority[lightID]
			v.Generation++
			v.Kind = "manual"
			v.ID = id
			v.Version = 0
			v.Acknowledged = false
			c.memoryAuthority[lightID] = v
			out[lightID] = v
		}
		return out, nil
	}
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	for _, lightID := range ids {
		_, err = tx.ExecContext(ctx, `INSERT INTO hue_target_authority_v1(light_id,generation,kind,resource_id,version,acknowledged) VALUES(?,1,'manual',?,0,0) ON CONFLICT(light_id) DO UPDATE SET generation=generation+1,kind='manual',resource_id=excluded.resource_id,version=0,acknowledged=0`, lightID, id)
		if err != nil {
			return nil, err
		}
		var v targetAuthority
		if err = tx.QueryRowContext(ctx, `SELECT generation,kind,resource_id,version,acknowledged FROM hue_target_authority_v1 WHERE light_id=?`, lightID).Scan(&v.Generation, &v.Kind, &v.ID, &v.Version, &v.Acknowledged); err != nil {
			return nil, err
		}
		out[lightID] = v
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return out, nil
}
func (c *Client) targetLocked(ctx context.Context, id string) (targetAuthority, error) {
	if c.db == nil {
		return c.memoryAuthority[id], nil
	}
	var a targetAuthority
	err := c.db.QueryRowContext(ctx, `SELECT generation,kind,resource_id,version,acknowledged FROM hue_target_authority_v1 WHERE light_id=?`, id).Scan(&a.Generation, &a.Kind, &a.ID, &a.Version, &a.Acknowledged)
	return a, err
}
func (c *Client) targetSend(ctx context.Context, id string, tokens map[string]targetAuthority, send func() error) error {
	release, err := c.AcquireCommand(ctx)
	if err != nil {
		return err
	}
	defer release()
	a, err := c.targetLocked(ctx, id)
	if err != nil {
		return fmt.Errorf("load target authority (re-publish desired state after migration): %w", err)
	}
	if d, ok := desiredRequest(ctx); ok {
		if a.Kind != d.Kind || a.ID != d.ID || a.Version != d.Version {
			return ErrSuperseded
		}
	} else {
		want, ok := tokens[id]
		if !ok || a.Generation != want.Generation || a.ID != want.ID {
			return ErrSuperseded
		}
	}
	if a.Acknowledged {
		return nil
	}
	if err = send(); err != nil {
		return err
	}
	if c.db == nil {
		a.Acknowledged = true
		c.memoryAuthority[id] = a
		return nil
	}
	_, err = c.db.ExecContext(ctx, `UPDATE hue_target_authority_v1 SET acknowledged=1 WHERE light_id=? AND generation=?`, id, a.Generation)
	if err != nil {
		return fmt.Errorf("Hue accepted write but persisting acknowledgment failed: %w", err)
	}
	return nil
}
