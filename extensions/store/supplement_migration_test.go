package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

func TestSchemaNinePreservesExistingReviewFacts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	raw, e := sql.Open("sqlite", path)
	if e != nil {
		t.Fatal(e)
	}
	_, e = raw.Exec("CREATE TABLE ext_schema_migrations(version INTEGER PRIMARY KEY)")
	if e != nil {
		t.Fatal(e)
	}
	for i, body := range []string{lifecycleMigration, ingestTagMigration, parserMigration, lifecyclePolicyMigration, reviewAlertsMigration, provenanceMigration, lifecycleConsistencyMigration, lineageSourcesMigration} {
		if _, e = raw.Exec(body); e != nil {
			t.Fatal(e)
		}
		if _, e = raw.Exec("INSERT INTO ext_schema_migrations VALUES(?)", i+1); e != nil {
			t.Fatal(e)
		}
	}
	_, e = raw.Exec(`INSERT INTO ext_document_annotations VALUES('7','kb','doc',1,NULL,NULL,'2000-01-01T00:00:00Z','{"user":"keep"}');INSERT INTO ext_review_alerts VALUES('alert','7','kb','doc','2000-01-01T00:00:00Z',1,3,'open','2026-11-01T00:00:00Z',NULL,NULL)`)
	if e != nil {
		t.Fatal(e)
	}
	raw.Close()
	db, e := OpenDurable(context.Background(), path)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	e = db.View(context.Background(), func(tx *sql.Tx) error {
		var generation int
		var labels string
		if e := tx.QueryRow("SELECT generation FROM ext_review_alerts WHERE alert_id='alert'").Scan(&generation); e != nil {
			return e
		}
		if generation != 3 {
			t.Fatal(generation)
		}
		if e := tx.QueryRow("SELECT labels_json FROM ext_document_annotations WHERE document_id='doc'").Scan(&labels); e != nil {
			return e
		}
		if labels != `{"user":"keep"}` {
			t.Fatal(labels)
		}
		rows, e := tx.Query("PRAGMA foreign_key_check")
		if e != nil {
			return e
		}
		defer rows.Close()
		if rows.Next() {
			t.Fatal("broken foreign key")
		}
		return rows.Err()
	})
	if e != nil {
		t.Fatal(e)
	}
}
