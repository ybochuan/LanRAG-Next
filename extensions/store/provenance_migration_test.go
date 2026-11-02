package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

func TestUpgradeProvenanceFromTrackedSchemaFive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v5.db")
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, migration := range []string{lifecycleMigration, ingestTagMigration, parserMigration, lifecyclePolicyMigration, reviewAlertsMigration} {
		if _, err = raw.Exec(migration); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = raw.Exec("CREATE TABLE ext_schema_migrations(version INTEGER PRIMARY KEY); INSERT INTO ext_schema_migrations VALUES(1),(2),(3),(4),(5); INSERT INTO ext_lifecycle_rules VALUES('7','preserved',1,'{}')"); err != nil {
		t.Fatal(err)
	}
	raw.Close()
	db, err := OpenDurable(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.View(context.Background(), func(tx *sql.Tx) error {
		var version, kept int
		if e := tx.QueryRow("SELECT MAX(version) FROM ext_schema_migrations").Scan(&version); e != nil {
			return e
		}
		if e := tx.QueryRow("SELECT revision FROM ext_lifecycle_rules WHERE knowledge_base_id='preserved'").Scan(&kept); e != nil {
			return e
		}
		if version != 11 || kept != 1 {
			t.Fatal(version, kept)
		}
		rows, e := tx.Query("PRAGMA foreign_key_check")
		if e != nil {
			return e
		}
		defer rows.Close()
		if rows.Next() {
			t.Fatal("foreign key broken")
		}
		return rows.Err()
	}); err != nil {
		t.Fatal(err)
	}
}
