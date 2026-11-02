package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func TestLineageUpgradePreservesUnattestedSourceAndCopyFacts(t *testing.T) {
	for _, version := range []int{6, 7} {
		t.Run(fmt.Sprint(version), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "legacy.db")
			raw, e := sql.Open("sqlite", path)
			if e != nil {
				t.Fatal(e)
			}
			ddl := []string{lifecycleMigration, ingestTagMigration, parserMigration, lifecyclePolicyMigration, reviewAlertsMigration, provenanceMigration, lifecycleConsistencyMigration}
			if _, e = raw.Exec("CREATE TABLE ext_schema_migrations(version INTEGER PRIMARY KEY)"); e != nil {
				t.Fatal(e)
			}
			for i, m := range ddl[:version] {
				if _, e = raw.Exec(m); e != nil {
					t.Fatal(e)
				}
				if _, e = raw.Exec("INSERT INTO ext_schema_migrations VALUES(?)", i+1); e != nil {
					t.Fatal(e)
				}
			}
			if _, e = raw.Exec(`INSERT INTO ext_lifecycle_events(event_id,tenant_id,knowledge_base_id,document_id,from_state,to_state,reason,occurred_at,stage,attempt,payload_json) VALUES('anchor','7','kb','doc','','active','fixture','2026-11-01T00:00:00Z','fixture',0,'{}');INSERT INTO ext_lifecycle_current VALUES('7','kb','doc','active','anchor')`); e != nil {
				t.Fatal(e)
			}
			if _, e = raw.Exec("INSERT INTO ext_provenance_sources VALUES('7','kb','doc','legacy',?,9,'anchor',?)", strings.Repeat("a", 64), strings.Repeat("b", 64)); e != nil {
				t.Fatal(e)
			}
			if _, e = raw.Exec("INSERT INTO ext_provenance_source_heads VALUES('7','kb','doc',1,'legacy',9)"); e != nil {
				t.Fatal(e)
			}
			body, e := json.Marshal(map[string]any{"scope": map[string]any{"tenant_id": 7, "knowledge_base_id": "kb"}, "copy_id": "copy", "revision": 1, "status": "ready", "sources": []any{map[string]any{"document_id": "doc", "version_id": "legacy"}}, "parents": []any{}, "binding": map[string]any{"user_id": "user", "session_id": "session", "resource_id": "resource", "content_version": "v1", "sha256": strings.Repeat("a", 64)}, "evidence_hash": strings.Repeat("b", 64), "run_id": "old-run", "created_at": "2026-11-01T00:00:00Z"})
			if e != nil {
				t.Fatal(e)
			}
			if _, e = raw.Exec("INSERT INTO ext_provenance_manifests VALUES('7','kb','copy',1,'ready',?)", string(body)); e != nil {
				t.Fatal(e)
			}
			if _, e = raw.Exec("INSERT INTO ext_provenance_copy_heads VALUES('7','kb','copy',1);INSERT INTO ext_provenance_manifest_sources VALUES('7','kb','copy',1,'doc','legacy')"); e != nil {
				t.Fatal(e)
			}
			if e = raw.Close(); e != nil {
				t.Fatal(e)
			}
			db, e := OpenDurable(context.Background(), path)
			if e != nil {
				t.Fatal(e)
			}
			defer db.Close()
			e = db.View(context.Background(), func(tx *sql.Tx) error {
				var count int
				var payload, head string
				var seq int
				if e := tx.QueryRow("SELECT MAX(version) FROM ext_schema_migrations").Scan(&count); e != nil {
					return e
				}
				if count != 11 {
					t.Fatal(count)
				}
				if e := tx.QueryRow("SELECT payload_json FROM ext_provenance_manifests").Scan(&payload); e != nil {
					return e
				}
				if payload != string(body) {
					t.Fatal("legacy manifest rewritten")
				}
				if e := tx.QueryRow("SELECT version_id,native_sequence FROM ext_provenance_source_heads").Scan(&head, &seq); e != nil {
					return e
				}
				if head != "legacy" || seq != 9 {
					t.Fatal("legacy source changed")
				}
				if e := tx.QueryRow("SELECT COUNT(*) FROM ext_provenance_manifest_sources").Scan(&count); e != nil {
					return e
				}
				if count != 1 {
					t.Fatal("source reference lost")
				}
				if e := tx.QueryRow("SELECT COUNT(*) FROM ext_source_snapshot_evidence").Scan(&count); e != nil {
					return e
				}
				if count != 0 {
					t.Fatal("migration fabricated trusted evidence")
				}
				rows, e := tx.Query("PRAGMA foreign_key_check")
				if e != nil {
					return e
				}
				defer rows.Close()
				if rows.Next() {
					t.Fatal("broken FK")
				}
				return rows.Err()
			})
			if e != nil {
				t.Fatal(e)
			}
		})
	}
}
