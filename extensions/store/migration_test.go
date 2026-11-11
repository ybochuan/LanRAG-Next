package store

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

func TestSchema28UpgradePreservesLegacyFeedbackAndBackup(t *testing.T) {
	dir := t.TempDir()
	backupPath := filepath.Join(dir, "extension-v27.db")
	raw, err := sql.Open("sqlite", backupPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = raw.Close() })
	raw.SetMaxOpenConns(1)
	if _, err := raw.Exec("PRAGMA foreign_keys=ON; CREATE TABLE ext_schema_migrations(version INTEGER PRIMARY KEY)"); err != nil {
		t.Fatal(err)
	}
	migrations, err := filepath.Glob("migrations/*.sql")
	const previous = 27
	if err != nil || len(migrations) < previous || filepath.Base(migrations[previous-1]) != "027_family_suggestions.sql" {
		t.Fatalf("schema 27 fixture unavailable: %v", err)
	}
	for i, path := range migrations[:previous] {
		migration, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := raw.Exec(string(migration)); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		if _, err := raw.Exec("INSERT INTO ext_schema_migrations VALUES(?)", i+1); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := raw.Exec(`
INSERT INTO ext_feedback_events VALUES('7','kb','qa','manual','fixture','original opinion',1,'2026-11-10T00:00:00Z');
INSERT INTO ext_feedback_staging VALUES('7','kb','qa','fixture','original opinion','medium','active',2,'2026-11-10T00:00:00Z');
INSERT INTO ext_feedback_guidances VALUES('7','kb','guidance','qa','feedback','fixture','prior guidance',1,'active',1,'2026-11-10T00:00:00Z');
INSERT INTO ext_provenance_runs VALUES('7','kb','run',1,'sealed',lower(hex(zeroblob(32))),'{}');
INSERT INTO ext_feedback_qa_bindings VALUES('7','kb','qa','run','["chunk"]');`); err != nil {
		t.Fatal(err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}
	backup, err := os.ReadFile(backupPath)
	if err != nil {
		t.Fatal(err)
	}
	candidatePath := filepath.Join(dir, "extension.db")
	if err := os.WriteFile(candidatePath, backup, 0600); err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	for i := 0; i < 2; i++ {
		db, err := OpenDurable(ctx, candidatePath)
		if err != nil {
			t.Fatal(err)
		}
		err = db.View(ctx, func(tx *sql.Tx) error {
			var version, contextCount, violations int
			var integrity, opinion, status, guidance, chunks string
			err := tx.QueryRow(`
SELECT (SELECT max(version) FROM ext_schema_migrations),
       (SELECT count(*) FROM ext_feedback_review_context),
       (SELECT count(*) FROM pragma_foreign_key_check),
       (SELECT * FROM pragma_integrity_check),
       f.text,f.status,g.text,b.retrieved_chunk_ids
FROM ext_feedback_staging f JOIN ext_feedback_guidances g USING(tenant_id,knowledge_base_id,feedback_id)
JOIN ext_feedback_qa_bindings b USING(tenant_id,knowledge_base_id,feedback_id)
WHERE f.tenant_id='7' AND f.knowledge_base_id='kb' AND f.feedback_id='qa'`).Scan(
				&version, &contextCount, &violations, &integrity, &opinion, &status, &guidance, &chunks)
			if err == nil && (version != LatestSchemaVersion || contextCount != 0 || violations != 0 || integrity != "ok" ||
				opinion != "original opinion" || status != "active" || guidance != "prior guidance" || chunks != `["chunk"]`) {
				t.Errorf("upgraded feedback or schema differs: version=%d contexts=%d FK=%d integrity=%s", version, contextCount, violations, integrity)
			}
			return err
		})
		_ = db.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	after, err := os.ReadFile(backupPath)
	if err != nil || string(after) != string(backup) {
		t.Fatal("original schema 27 backup changed", err)
	}
}

func TestMigrationUpgradesV1WithoutLosingData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agentrag_ext.db")
	v1, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := v1.Exec("CREATE TABLE ext_schema_migrations(version INTEGER PRIMARY KEY); INSERT INTO ext_schema_migrations VALUES(1);" + lifecycleMigration + "CREATE TABLE ext_upgrade_probe(id INTEGER); INSERT INTO ext_upgrade_probe VALUES(42)"); err != nil {
		t.Fatal(err)
	}
	if err := v1.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.View(context.Background(), func(tx *sql.Tx) error {
		var version, id int
		if err := tx.QueryRow("SELECT MAX(version) FROM ext_schema_migrations").Scan(&version); err != nil {
			return err
		}
		if err := tx.QueryRow("SELECT id FROM ext_upgrade_probe").Scan(&id); err != nil {
			return err
		}
		if version != LatestSchemaVersion || id != 42 {
			t.Errorf("version=%d id=%d", version, id)
		}
		for _, table := range []string{"ext_ingest_current", "ext_ingest_observations", "ext_kb_tag_mapping", "ext_tag_sync_failures", "ext_parser_documents", "ext_parser_jobs", "ext_parser_artifacts", "ext_lifecycle_rules", "ext_document_annotations", "ext_lifecycle_decisions", "ext_review_alerts"} {
			if _, err := tx.Exec("SELECT * FROM " + table); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestMigrationUpgradesV2ParserWithoutLosingData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "extension.db")
	old, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = old.Exec("CREATE TABLE ext_schema_migrations(version INTEGER PRIMARY KEY); INSERT INTO ext_schema_migrations VALUES(1),(2);" + lifecycleMigration + ingestTagMigration + "CREATE TABLE ext_preserved(id INTEGER); INSERT INTO ext_preserved VALUES(42)")
	_ = old.Close()
	if err != nil {
		t.Fatal(err)
	}
	db, err := OpenDurable(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.View(context.Background(), func(tx *sql.Tx) error {
		var version, id int
		if err := tx.QueryRow("SELECT MAX(version) FROM ext_schema_migrations").Scan(&version); err != nil {
			return err
		}
		if err := tx.QueryRow("SELECT id FROM ext_preserved").Scan(&id); err != nil {
			return err
		}
		if version != LatestSchemaVersion || id != 42 {
			t.Errorf("migration version=%d preserved=%d", version, id)
		}
		_, err := tx.Exec("SELECT * FROM ext_parser_artifacts")
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

func TestMigrationV3PreservesParserAndLifecycleFacts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "extension.db")
	old, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = old.Exec(`CREATE TABLE ext_schema_migrations(version INTEGER PRIMARY KEY); INSERT INTO ext_schema_migrations VALUES(1),(2),(3);` + lifecycleMigration + ingestTagMigration + parserMigration + `
 INSERT INTO ext_parser_documents VALUES('identity',7,'kb','doc',X'7b7d','2026-10-31T00:00:00Z');
 INSERT INTO ext_parser_jobs VALUES('identity','job',200,X'7b7d');
 INSERT INTO ext_parser_artifacts VALUES('identity','job',7,'kb','doc','file','middle_json','2','4.0.1','hash',X'7b7d','2026-10-31T00:00:00Z');
 INSERT INTO ext_lifecycle_events(event_id,tenant_id,knowledge_base_id,document_id,from_state,to_state,reason,occurred_at,stage,attempt,payload_json) VALUES('old-event','7','kb','doc','','pending','old','2026-10-31T00:00:00Z','',0,'{}');
 INSERT INTO ext_lifecycle_current VALUES('7','kb','doc','pending','old-event');`)
	_ = old.Close()
	if err != nil {
		t.Fatal(err)
	}
	db, err := OpenDurable(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.View(context.Background(), func(tx *sql.Tx) error {
		var event, version string
		var data []byte
		if err := tx.QueryRow("SELECT last_event_id FROM ext_lifecycle_current").Scan(&event); err != nil {
			return err
		}
		if err := tx.QueryRow("SELECT mineru_version,middle_json FROM ext_parser_artifacts").Scan(&version, &data); err != nil {
			return err
		}
		if event != "old-event" || version != "4.0.1" || string(data) != "{}" {
			t.Fatalf("data lost: %s %s %s", event, version, data)
		}
		var n int
		if err := tx.QueryRow("SELECT count(*) FROM ext_document_annotations").Scan(&n); err != nil {
			return err
		}
		if n != 0 {
			t.Fatal("migration invented annotations")
		}
		rows, err := tx.Query("PRAGMA foreign_key_check")
		if err != nil {
			return err
		}
		defer rows.Close()
		if rows.Next() {
			t.Fatal("broken FK after upgrade")
		}
		return rows.Err()
	}); err != nil {
		t.Fatal(err)
	}
}

func TestMigrationV4PreservesAnnotationsWithoutInventingAlerts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ext.db")
	old, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = old.Exec(`CREATE TABLE ext_schema_migrations(version INTEGER PRIMARY KEY);INSERT INTO ext_schema_migrations VALUES(1),(2),(3),(4);` + lifecycleMigration + ingestTagMigration + parserMigration + lifecyclePolicyMigration + `INSERT INTO ext_document_annotations VALUES('7','kb','doc',7,NULL,NULL,'2026-10-30T00:00:00Z','{"business":"preserved"}');`)
	old.Close()
	if err != nil {
		t.Fatal(err)
	}
	db, err := OpenDurable(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.View(context.Background(), func(tx *sql.Tx) error {
		var revision, n int
		var due, labels string
		if err := tx.QueryRow("SELECT revision,review_due,labels_json FROM ext_document_annotations").Scan(&revision, &due, &labels); err != nil {
			return err
		}
		if revision != 7 || due != "2026-10-30T00:00:00Z" || labels != `{"business":"preserved"}` {
			t.Fatal("v4 annotation changed")
		}
		if err := tx.QueryRow("SELECT COUNT(*) FROM ext_review_alerts").Scan(&n); err != nil {
			return err
		}
		if n != 0 {
			t.Fatal("migration emitted alert")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
