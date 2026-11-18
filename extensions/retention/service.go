// Package retention prunes old provenance evidence. It never removes the
// sealed run head, lifecycle facts or governance state: feedback bindings keep
// their foreign key to ext_provenance_runs.
package retention

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/store"
)

var ErrScope = errors.New("retention scope denied")

// Result reports one bounded sweep for one scope.
type Result struct {
	RunID              string `json:"run_id"`
	TenantID           uint64 `json:"tenant_id"`
	KnowledgeBaseID    string `json:"knowledge_base_id"`
	RunsScanned        int    `json:"runs_scanned"`
	ReceiptsDeleted    int    `json:"receipts_deleted"`
	AuditDeleted       int    `json:"audit_deleted"`
	InputsDeleted      int    `json:"inputs_deleted"`
	IntentsDeleted     int    `json:"intents_deleted"`
	StepsDeleted       int    `json:"steps_deleted"`
	EvidenceDeleted    int    `json:"evidence_deleted"`
	TagFailuresDeleted int    `json:"tag_failures_deleted"`
}

type Service struct {
	db        *store.DB
	tenant    uint64
	kbs       map[string]bool
	retention time.Duration
	batch     int
	now       func() time.Time
	ready     func(context.Context) error
}

// New binds retention to the configured tenant/KB scope. retention <= 0 uses 30 days.
func New(db *store.DB, tenant uint64, kbs []string, retention time.Duration) *Service {
	allowed := make(map[string]bool, len(kbs))
	for _, kb := range kbs {
		allowed[kb] = true
	}
	if retention <= 0 {
		retention = 30 * 24 * time.Hour
	}
	return &Service{db: db, tenant: tenant, kbs: allowed, retention: retention, batch: 200, now: time.Now}
}

// WithNow is used by tests; production keeps the system clock.
func (s *Service) WithNow(now func() time.Time) *Service {
	if now != nil {
		s.now = now
	}
	return s
}

// WithReady delays the first sweep until an external dependency is available.
func (s *Service) WithReady(ready func(context.Context) error) *Service {
	s.ready = ready
	return s
}

// Sweep prunes evidence for every configured KB and returns one result per KB.
func (s *Service) Sweep(ctx context.Context, trigger string) ([]Result, error) {
	cutoffTime := s.now().UTC().Add(-s.retention)
	cutoff := cutoffTime.Format(time.RFC3339Nano)
	cutoffMillis := cutoffTime.UnixMilli()
	out := []Result{}
	var allErr error
	for kb := range s.kbs {
		result, err := s.sweepKB(ctx, kb, cutoff, cutoffMillis, trigger)
		if result != nil {
			out = append(out, *result)
		}
		if err != nil && ctx.Err() == nil {
			allErr = errors.Join(allErr, err)
		}
	}
	return out, allErr
}

func (s *Service) sweepKB(ctx context.Context, kb, cutoff string, cutoffMillis int64, trigger string) (*Result, error) {
	now := s.now().UTC().Format(time.RFC3339Nano)
	tenant := strconv.FormatUint(s.tenant, 10)
	result := &Result{RunID: runID(tenant, kb, now, trigger), TenantID: s.tenant, KnowledgeBaseID: kb}
	if err := s.db.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "INSERT INTO ext_evidence_retention_runs(run_id,tenant_id,knowledge_base_id,started_at,cutoff_at,trigger_name) VALUES(?,?,?,?,?,?)", result.RunID, tenant, kb, now, cutoff, trigger)
		return err
	}); err != nil {
		return nil, err
	}
	var sweepErr error
	for {
		var deleted int
		err := s.db.Write(ctx, func(tx *sql.Tx) error {
			n, err := s.pruneBatch(ctx, tx, tenant, kb, cutoff, cutoffMillis, result)
			deleted = n
			return err
		})
		if err != nil {
			sweepErr = errors.Join(sweepErr, err)
			break
		}
		if deleted == 0 {
			break
		}
	}
	if err := s.db.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "UPDATE ext_evidence_retention_runs SET finished_at=?,runs_scanned=?,receipts_deleted=?,audit_deleted=?,inputs_deleted=?,intents_deleted=?,steps_deleted=?,evidence_deleted=?,tag_failures_deleted=?,last_error=? WHERE run_id=?", s.now().UTC().Format(time.RFC3339Nano), result.RunsScanned, result.ReceiptsDeleted, result.AuditDeleted, result.InputsDeleted, result.IntentsDeleted, result.StepsDeleted, result.EvidenceDeleted, result.TagFailuresDeleted, errText(sweepErr), result.RunID)
		return err
	}); err != nil {
		sweepErr = errors.Join(sweepErr, err)
	}
	return result, sweepErr
}

func runID(tenant, kb, now, trigger string) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{tenant, kb, now, trigger}, "|")))
	return hex.EncodeToString(sum[:16])
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// pruneBatch deletes at most batch child rows. Run heads and feedback bindings
// are deliberately untouched so governance facts remain verifiable by identity.
func (s *Service) pruneBatch(ctx context.Context, tx *sql.Tx, tenant, kb, cutoff string, cutoffMillis int64, result *Result) (int, error) {
	rows, err := tx.QueryContext(ctx, "SELECT r.run_id FROM ext_provenance_runs r WHERE r.tenant_id=? AND r.knowledge_base_id=? AND r.state IN ('sealed','failed','unattributed') AND NOT EXISTS (SELECT 1 FROM ext_provenance_run_audit a WHERE a.tenant_id=r.tenant_id AND a.knowledge_base_id=r.knowledge_base_id AND a.run_id=r.run_id AND a.occurred_at>?) ORDER BY r.run_id LIMIT ?", tenant, kb, cutoff, s.batch)
	if err != nil {
		return 0, err
	}
	runs := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		runs = append(runs, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	rows.Close()
	result.RunsScanned += len(runs)
	deleted := 0
	if len(runs) > 0 {
		ids, _ := json.Marshal(runs)
		// Child tables first: audit references receipts; inputs, intents and
		// producers reference steps. The run head itself is never deleted.
		stmts := []struct {
			sql    string
			target *int
		}{
			{"DELETE FROM ext_provenance_run_audit WHERE tenant_id=? AND knowledge_base_id=? AND run_id IN (SELECT value FROM json_each(?))", &result.AuditDeleted},
			{"DELETE FROM ext_provenance_run_receipts WHERE tenant_id=? AND knowledge_base_id=? AND run_id IN (SELECT value FROM json_each(?))", &result.ReceiptsDeleted},
			{"DELETE FROM ext_provenance_inputs WHERE tenant_id=? AND knowledge_base_id=? AND run_id IN (SELECT value FROM json_each(?))", &result.InputsDeleted},
			{"DELETE FROM ext_provenance_input_intents WHERE tenant_id=? AND knowledge_base_id=? AND run_id IN (SELECT value FROM json_each(?))", &result.IntentsDeleted},
			{"DELETE FROM ext_provenance_producers WHERE tenant_id=? AND knowledge_base_id=? AND run_id IN (SELECT value FROM json_each(?))", nil},
			{"DELETE FROM ext_guidance_calls WHERE tenant_id=? AND knowledge_base_id=? AND run_id IN (SELECT value FROM json_each(?))", nil},
			{"DELETE FROM ext_provenance_steps WHERE tenant_id=? AND knowledge_base_id=? AND run_id IN (SELECT value FROM json_each(?))", &result.StepsDeleted},
		}
		for _, stmt := range stmts {
			res, err := tx.ExecContext(ctx, stmt.sql, tenant, kb, string(ids))
			if err != nil {
				return deleted, err
			}
			n, err := res.RowsAffected()
			if err != nil {
				return deleted, err
			}
			deleted += int(n)
			if stmt.target != nil {
				*stmt.target += int(n)
			}
		}
	}
	// Snapshot evidence follows version observation time. Current versions stay.
	evidenceSQL := "DELETE FROM ext_source_snapshot_evidence WHERE tenant_id=? AND knowledge_base_id=? AND version_node_id IN (SELECT v.version_node_id FROM ext_document_version v WHERE v.tenant_id=? AND v.knowledge_base_id=? AND v.created_at<? AND NOT EXISTS (SELECT 1 FROM ext_document_version_family f WHERE f.tenant_id=v.tenant_id AND f.knowledge_base_id=v.knowledge_base_id AND (f.current_version_node_id=v.version_node_id OR f.root_version_node_id=v.version_node_id)) LIMIT ?)"
	if res, err := tx.ExecContext(ctx, evidenceSQL, tenant, kb, tenant, kb, cutoff, s.batch); err != nil {
		return deleted, err
	} else if n, e := res.RowsAffected(); e == nil {
		result.EvidenceDeleted += int(n)
		deleted += int(n)
	}
	failuresSQL := "DELETE FROM ext_tag_sync_failures WHERE rowid IN (SELECT rowid FROM ext_tag_sync_failures WHERE tenant_id=? AND knowledge_base_id=? AND occurred_at<? ORDER BY occurred_at LIMIT ?)"
	if res, err := tx.ExecContext(ctx, failuresSQL, tenant, kb, cutoffMillis, s.batch); err != nil {
		return deleted, err
	} else if n, e := res.RowsAffected(); e == nil {
		result.TagFailuresDeleted += int(n)
		deleted += int(n)
	}
	return deleted, nil
}

// Run sweeps immediately then every interval until cancellation.
func (s *Service) Run(ctx context.Context, interval time.Duration, report func(error)) error {
	if interval <= 0 {
		interval = 24 * time.Hour
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	if s.ready != nil {
		for {
			if err := s.ready(ctx); err == nil {
				break
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(5 * time.Second):
			}
		}
	}
	for ctx.Err() == nil {
		if _, err := s.Sweep(ctx, "scheduled"); err != nil && ctx.Err() == nil {
			report(err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
	return ctx.Err()
}
