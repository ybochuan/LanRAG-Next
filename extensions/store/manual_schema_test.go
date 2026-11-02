package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

func TestManualSchemaAdoption(t *testing.T) {
	for _, version := range []int{1, 3, 5, 6, 7, 8, 9, 10} {
		t.Run(string(rune('0'+version)), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "manual.db")
			raw, err := sql.Open("sqlite", path)
			if err != nil {
				t.Fatal(err)
			}
			migrations := []string{lifecycleMigration, ingestTagMigration, parserMigration, lifecyclePolicyMigration, reviewAlertsMigration, provenanceMigration, lifecycleConsistencyMigration, lineageSourcesMigration, supplementsMigration, provenanceRunsMigration}
			for _, migration := range migrations[:version] {
				if _, err := raw.Exec(migration); err != nil {
					t.Fatal(err)
				}
			}
			if version >= 5 {
				if _, err := raw.Exec(`INSERT INTO ext_lifecycle_rules VALUES('7','keep-me',1,'{}')`); err != nil {
					t.Fatal(err)
				}
			}
			raw.Close()
			db, err := OpenDurable(context.Background(), path)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			if version >= 5 {
				if err := db.View(context.Background(), func(tx *sql.Tx) error {
					var revision int
					return tx.QueryRow(`SELECT revision FROM ext_lifecycle_rules WHERE knowledge_base_id='keep-me'`).Scan(&revision)
				}); err != nil {
					t.Fatal("manual facts lost:", err)
				}
			}
			if err := db.View(context.Background(), func(tx *sql.Tx) error {
				var n int
				err := tx.QueryRow("SELECT COUNT(*) FROM ext_schema_migrations").Scan(&n)
				if n != 11 {
					t.Errorf("migration count=%d", n)
				}
				return err
			}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestManualSchemaMismatchDoesNotStampHistory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "manual.db")
	raw, _ := sql.Open("sqlite", path)
	defer raw.Close()
	if _, err := raw.Exec("CREATE TABLE ext_lifecycle_events (unsafe TEXT)"); err != nil {
		t.Fatal(err)
	}
	if db, err := Open(context.Background(), path); err == nil {
		db.Close()
		t.Fatal("accepted incompatible manual schema")
	}
	var count int
	if err := raw.QueryRow("SELECT COUNT(*) FROM sqlite_schema WHERE name='ext_schema_migrations'").Scan(&count); err != nil || count != 0 {
		t.Fatalf("history changed count=%d err=%v", count, err)
	}
}
