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
		if version != 2 || id != 42 {
			t.Errorf("version=%d id=%d", version, id)
		}
		for _, table := range []string{"ext_ingest_current", "ext_ingest_observations", "ext_kb_tag_mapping", "ext_tag_sync_failures"} {
			if _, err := tx.Exec("SELECT * FROM " + table); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
