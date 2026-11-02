package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

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
		if version != 11 || id != 42 {
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
		if version != 11 || id != 42 {
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
