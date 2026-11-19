package retention

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/store"
)

const (
	testTenant = 7
	testKB     = "kb-retention"
)

func hash(c string) string { return strings.Repeat(c, 64) }

func fixture(t *testing.T, now time.Time) *store.DB {
	t.Helper()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "ext.db")
	db, err := store.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	old := now.Add(-40 * 24 * time.Hour).UTC().Format(time.RFC3339Nano)
	fresh := now.Add(-2 * time.Hour).UTC().Format(time.RFC3339Nano)
	stamp := "2000-01-01T00:00:00Z"
	// Insert order follows foreign keys: outbox, lineage, lifecycle event,
	// versions, family, runs and their child evidence.
	stmts := []string{
		"INSERT INTO ext_projection_outbox(outbox_id,tenant_id,knowledge_base_id,projection_type,aggregate_type,aggregate_id,causation_type,causation_id,payload_json,status,retry_count,next_retry_at,last_error,created_at) VALUES('o1','7','kb-retention','tag_projection','document','doc','event','e1','{}','done',0,'" + stamp + "','','" + stamp + "')",
		"INSERT INTO ext_projection_outbox(outbox_id,tenant_id,knowledge_base_id,projection_type,aggregate_type,aggregate_id,causation_type,causation_id,payload_json,status,retry_count,next_retry_at,last_error,created_at) VALUES('o2','7','kb-retention','tag_projection','document','doc','event','e2','{}','done',0,'" + stamp + "','','" + stamp + "')",
		"INSERT INTO ext_lineage_node VALUES('version-old','7','kb-retention','document_version','doc','" + old + "')",
		"INSERT INTO ext_lineage_node VALUES('version-fresh','7','kb-retention','document_version','doc','" + fresh + "')",
		"INSERT INTO ext_lifecycle_events(sequence,event_id,tenant_id,knowledge_base_id,document_id,from_state,to_state,reason,occurred_at,stage,attempt,payload_json) VALUES(1,'life-1','7','kb-retention','doc','','active','test','" + stamp + "','fixture',0,'{}')",
		"INSERT INTO ext_document_version_family VALUES('fam','7','kb-retention','doc','Fixture','version-old','version-fresh',1,'" + stamp + "','" + stamp + "')",
		"INSERT INTO ext_document_version VALUES('version-old','7','kb-retention','fam','doc','" + hash("3") + "',1,'life-1','','fixture','" + old + "')",
		"INSERT INTO ext_document_version VALUES('version-fresh','7','kb-retention','fam','doc','" + hash("4") + "',2,'life-1','','fixture','" + fresh + "')",
		"INSERT INTO ext_source_snapshot_evidence VALUES('7','kb-retention','version-old','{}','" + hash("1") + "')",
		"INSERT INTO ext_source_snapshot_evidence VALUES('7','kb-retention','version-fresh','{}','" + hash("2") + "')",
		"INSERT INTO ext_provenance_runs VALUES('7','kb-retention','old-run',2,'sealed','" + hash("a") + "','{}')",
		"INSERT INTO ext_provenance_runs VALUES('7','kb-retention','fresh-run',2,'sealed','" + hash("b") + "','{}')",
		"INSERT INTO ext_provenance_run_receipts VALUES('7','kb-retention','op-old','old-run','begin','" + hash("c") + "','{}')",
		"INSERT INTO ext_provenance_run_receipts VALUES('7','kb-retention','op-fresh','fresh-run','begin','" + hash("c") + "','{}')",
		"INSERT INTO ext_provenance_run_audit(sequence,tenant_id,knowledge_base_id,operation_id,run_id,revision,state,occurred_at) VALUES(1,'7','kb-retention','op-old','old-run',2,'sealed','" + old + "')",
		"INSERT INTO ext_provenance_run_audit(sequence,tenant_id,knowledge_base_id,operation_id,run_id,revision,state,occurred_at) VALUES(2,'7','kb-retention','op-fresh','fresh-run',2,'sealed','" + fresh + "')",
		"INSERT INTO ext_provenance_steps VALUES('7','kb-retention','old-run','step-old','model','success','" + hash("d") + "','" + hash("e") + "')",
		"INSERT INTO ext_provenance_inputs VALUES('7','kb-retention','old-run','step-old','op-old','" + hash("f") + "','[]','chunk-1')",
		"INSERT INTO ext_tag_sync_failures VALUES(1,'o1','7','kb-retention','doc','reason'," + strconv.FormatInt(now.Add(-40*24*time.Hour).UnixMilli(), 10) + ")",
		"INSERT INTO ext_tag_sync_failures VALUES(2,'o2','7','kb-retention','doc','reason'," + strconv.FormatInt(now.Add(-2*time.Hour).UnixMilli(), 10) + ")",
	}
	if err := db.Write(ctx, func(tx *sql.Tx) error {
		for i, stmt := range stmts {
			if _, err := tx.ExecContext(ctx, stmt); err != nil {
				return fmt.Errorf("fixture stmt %d: %w", i, err)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestSweepPrunesOldEvidenceButKeepsRunHeads(t *testing.T) {
	now := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	db := fixture(t, now)
	s := New(db, testTenant, []string{testKB}, 30*24*time.Hour).WithNow(func() time.Time { return now })
	ctx := context.Background()
	results, err := s.Sweep(ctx, "test")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("results=%+v", results)
	}
	r := results[0]
	if r.AuditDeleted != 1 || r.ReceiptsDeleted != 1 || r.InputsDeleted != 1 || r.StepsDeleted != 1 {
		t.Fatalf("old evidence not pruned: %+v", r)
	}
	if r.TagFailuresDeleted != 1 {
		t.Fatalf("tag failures not pruned: %+v", r)
	}
	// version-old is the family root; its evidence must be preserved.
	if r.EvidenceDeleted != 0 {
		t.Fatalf("protected version evidence pruned: %+v", r)
	}
	var runs, freshAudit, freshSteps, oldRows int
	if err := db.View(ctx, func(tx *sql.Tx) error {
		if err := tx.QueryRow("SELECT count(*) FROM ext_provenance_runs").Scan(&runs); err != nil {
			return err
		}
		if err := tx.QueryRow("SELECT count(*) FROM ext_provenance_run_audit WHERE run_id='fresh-run'").Scan(&freshAudit); err != nil {
			return err
		}
		if err := tx.QueryRow("SELECT count(*) FROM ext_provenance_steps WHERE run_id='fresh-run'").Scan(&freshSteps); err != nil {
			return err
		}
		return tx.QueryRow("SELECT count(*) FROM ext_provenance_run_audit WHERE run_id='old-run'").Scan(&oldRows)
	}); err != nil {
		t.Fatal(err)
	}
	if runs != 2 || freshAudit != 1 || freshSteps != 0 || oldRows != 0 {
		t.Fatalf("runs=%d freshAudit=%d freshSteps=%d oldRows=%d", runs, freshAudit, freshSteps, oldRows)
	}
	var auditRows int
	if err := db.View(ctx, func(tx *sql.Tx) error {
		return tx.QueryRow("SELECT count(*) FROM ext_evidence_retention_runs WHERE run_id=?", r.RunID).Scan(&auditRows)
	}); err != nil {
		t.Fatal(err)
	}
	if auditRows != 1 {
		t.Fatal("retention run not recorded")
	}
}

func TestSweepAdvancesPastPrunedRunsAndVersions(t *testing.T) {
	now := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	db := fixture(t, now)
	ctx := context.Background()
	old := now.AddDate(0, 0, -40).Format(time.RFC3339Nano)
	if err := db.Write(ctx, func(tx *sql.Tx) error {
		if _, err := tx.Exec(`INSERT INTO ext_provenance_runs VALUES ('7',?,'a-pruned',2,'sealed',?,'{}')`, testKB, hash("9")); err != nil {
			return err
		}
		for i, id := range []string{"historical-a", "historical-b"} {
			if _, err := tx.Exec(`INSERT INTO ext_lineage_node VALUES (?,'7',?,'document_version','doc',?)`, id, testKB, old); err != nil {
				return err
			}
			if _, err := tx.Exec(`INSERT INTO ext_document_version VALUES (?,'7',?,'fam','doc',?,?,'life-1','','fixture',?)`, id, testKB, hash("8"), i+3, old); err != nil {
				return err
			}
			if _, err := tx.Exec(`INSERT INTO ext_source_snapshot_evidence VALUES ('7',?,?,'{}',?)`, testKB, id, hash("7")); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(`INSERT INTO ext_guidance_calls VALUES ('7',?,'old-run','call','user','session',1,'[]')`, testKB); err != nil {
			return err
		}
		_, err := tx.Exec(`INSERT INTO ext_guidance_step_inputs VALUES ('7',?,'old-run','step-old','call',?)`, testKB, hash("6"))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	s := New(db, testTenant, []string{testKB}, 30*24*time.Hour).WithNow(func() time.Time { return now })
	s.batch = 1
	results, err := s.Sweep(ctx, "test")
	if err != nil || len(results) != 1 {
		t.Fatalf("results=%+v err=%v", results, err)
	}
	if r := results[0]; r.AuditDeleted != 1 || r.StepsDeleted != 1 || r.EvidenceDeleted != 2 {
		t.Fatalf("sweep missed evidence: %+v", r)
	}
	if err := db.View(ctx, func(tx *sql.Tx) error {
		var calls, roots int
		err := tx.QueryRow(`SELECT (SELECT count(*) FROM ext_guidance_calls),
			(SELECT count(*) FROM ext_source_snapshot_evidence WHERE version_node_id IN ('version-old','version-fresh'))`).Scan(&calls, &roots)
		if err == nil && (calls != 1 || roots != 2) {
			t.Fatalf("guidance budget or protected versions lost: calls=%d versions=%d", calls, roots)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}
