package reconcile

import (
	"database/sql"
	"fmt"
	"time"
)

// SetDB installs the durable delivery journal and restores handled versions and
// pending retries. It must run before starting the orchestrator.
func (o *Orchestrator) SetDB(db *sql.DB) error {
	if db == nil {
		return nil
	}
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS hue_delivery_v1 (
 kind TEXT NOT NULL,resource_id TEXT NOT NULL,version INTEGER NOT NULL,
 generation INTEGER NOT NULL DEFAULT 0,authority_generation INTEGER NOT NULL DEFAULT 0,status TEXT NOT NULL,
 attempts INTEGER NOT NULL DEFAULT 0,first_attempt INTEGER NOT NULL DEFAULT 0,
 next_attempt INTEGER NOT NULL DEFAULT 0,last_error TEXT NOT NULL DEFAULT '',
 updated_at INTEGER NOT NULL,PRIMARY KEY(kind,resource_id))`)
	if err != nil {
		return err
	}
	// Existing pre-journal rows have unknown physical acknowledgment. Treat them
	// as imported baselines, not queued lighting commands. A fresh desired frame
	// or explicit force is required to activate them.
	var journalCount, authorityCount int
	if err = db.QueryRow(`SELECT COUNT(*) FROM hue_delivery_v1`).Scan(&journalCount); err != nil {
		return err
	}
	if journalCount == 0 {
		var tableCount int
		if err = db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='hue_target_authority_v1'`).Scan(&tableCount); err != nil {
			return err
		}
		if tableCount > 0 {
			if err = db.QueryRow(`SELECT COUNT(*) FROM hue_target_authority_v1`).Scan(&authorityCount); err != nil {
				return err
			}
		}
		if authorityCount == 0 {
			var stateTable int
			if err = db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='resource_state'`).Scan(&stateTable); err != nil {
				return err
			}
			if stateTable > 0 {
				_, err = db.Exec(`INSERT INTO hue_delivery_v1(kind,resource_id,version,generation,authority_generation,status,updated_at) SELECT kind,id,version,0,0,'imported',? FROM resource_state WHERE kind IN ('group','light')`, time.Now().UnixMilli())
				if err != nil {
					return err
				}
			}
		}
	}
	rows, err := db.Query(`SELECT kind,resource_id,version,generation,authority_generation,status,attempts,first_attempt,next_attempt,last_error FROM hue_delivery_v1`)
	if err != nil {
		return err
	}
	defer rows.Close()
	o.mu.Lock()
	defer o.mu.Unlock()
	o.db = db
	for rows.Next() {
		var kind, id, status string
		var version, generation, authorityGeneration, first, next int64
		var attempts int
		var message string
		if err = rows.Scan(&kind, &id, &version, &generation, &authorityGeneration, &status, &attempts, &first, &next, &message); err != nil {
			return err
		}
		k := ResourceKey{Kind(kind), id}
		o.generations[k] = generation
		o.authorityGenerations[k] = authorityGeneration
		if status == "superseded" {
			o.superseded[k] = version
		}
		if status == "imported" {
			o.imported[k] = version
		}
		if status == "acknowledged" || status == "superseded" || status == "imported" {
			o.lastVersions[k] = version
			continue
		}
		retry := RetryStatus{Version: version, Attempts: attempts, Error: message, Expired: status == "expired"}
		if first != 0 {
			retry.FirstAttempt = time.UnixMilli(first)
		}
		if next != 0 {
			retry.Next = time.UnixMilli(next)
		}
		o.retries[k] = retry
		o.pending[k] = struct{}{}
	}
	return rows.Err()
}
func timestamp(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixMilli()
}
func (o *Orchestrator) persistLocked(k ResourceKey, version int64, status string, r RetryStatus) error {
	if o.db == nil {
		return nil
	}
	_, err := o.db.Exec(`INSERT INTO hue_delivery_v1(kind,resource_id,version,generation,authority_generation,status,attempts,first_attempt,next_attempt,last_error,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)
 ON CONFLICT(kind,resource_id) DO UPDATE SET version=excluded.version,generation=excluded.generation,authority_generation=excluded.authority_generation,status=excluded.status,attempts=excluded.attempts,first_attempt=excluded.first_attempt,next_attempt=excluded.next_attempt,last_error=excluded.last_error,updated_at=excluded.updated_at`, string(k.Kind), k.ID, version, o.generations[k], o.authorityGenerations[k], status, r.Attempts, timestamp(r.FirstAttempt), timestamp(r.Next), r.Error, time.Now().UnixMilli())
	if err != nil {
		o.persistenceError = fmt.Sprintf("persist Hue delivery: %v", err)
	} else {
		o.persistenceError = ""
	}
	return err
}

// ClearDelivery accompanies an explicit desired-state reset or migration.
func (o *Orchestrator) ClearDelivery() error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.db != nil {
		if _, err := o.db.Exec(`DELETE FROM hue_delivery_v1`); err != nil {
			return err
		}
	}
	o.lastVersions = map[ResourceKey]int64{}
	o.imported = map[ResourceKey]int64{}
	o.superseded = map[ResourceKey]int64{}
	o.pending = map[ResourceKey]struct{}{}
	o.retries = map[ResourceKey]RetryStatus{}
	o.generations = map[ResourceKey]int64{}
	o.authorityGenerations = map[ResourceKey]int64{}
	o.persistenceError = ""
	return nil
}
func (o *Orchestrator) forceLocked(k ResourceKey) {
	o.generations[k]++
	delete(o.imported, k)
	delete(o.superseded, k)
	o.pending[k] = struct{}{}
	delete(o.retries, k)
	r := RetryStatus{FirstAttempt: time.Now()}
	if err := o.persistLocked(k, 0, "pending", r); err != nil {
		o.retries[k] = RetryStatus{Error: err.Error(), FirstAttempt: r.FirstAttempt}
	}
}
