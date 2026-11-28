package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestMigrationCatalogIsContiguousAndChecksummed(t *testing.T) {
	if len(schemaMigrations) != LatestSchemaVersion {
		t.Fatalf("catalog length=%d latest=%d", len(schemaMigrations), LatestSchemaVersion)
	}
	for i, migration := range schemaMigrations {
		if migration.Version != i+1 || migration.SQL == "" || migration.Checksum == "" {
			t.Fatalf("migration %d is incomplete: %+v", i+1, migration)
		}
	}
}

func TestLegacyHistoryBackfillsChecksumsAndMatchesFreshSchema(t *testing.T) {
	dir := t.TempDir()
	oldPath := filepath.Join(dir, "old.db")
	raw, err := sql.Open("sqlite", oldPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec("CREATE TABLE ext_schema_migrations(version INTEGER PRIMARY KEY); INSERT INTO ext_schema_migrations VALUES(1);" + lifecycleMigration); err != nil {
		t.Fatal(err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}
	oldDB, err := Open(context.Background(), oldPath)
	if err != nil {
		t.Fatal(err)
	}
	defer oldDB.Close()
	var oldSnapshot map[string]string
	if err := oldDB.View(context.Background(), func(tx *sql.Tx) error {
		var err error
		oldSnapshot, err = schemaSnapshot(context.Background(), tx)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	freshDB, err := Open(context.Background(), filepath.Join(dir, "fresh.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer freshDB.Close()
	var freshSnapshot map[string]string
	if err := freshDB.View(context.Background(), func(tx *sql.Tx) error {
		var err error
		freshSnapshot, err = schemaSnapshot(context.Background(), tx)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(oldSnapshot, freshSnapshot) {
		t.Fatal("fresh and upgraded extension schemas differ")
	}
	if err := oldDB.View(context.Background(), func(tx *sql.Tx) error {
		var version, missing int
		if err := tx.QueryRow("SELECT MAX(version) FROM ext_schema_migrations").Scan(&version); err != nil {
			return err
		}
		if err := tx.QueryRow("SELECT COUNT(*) FROM ext_schema_migrations WHERE checksum=''").Scan(&missing); err != nil {
			return err
		}
		if version != LatestSchemaVersion || missing != 0 {
			t.Fatalf("version=%d missing_checksums=%d", version, missing)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestAppliedMigrationChecksumTamperIsRejected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tamper.db")
	db, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec("UPDATE ext_schema_migrations SET checksum='tampered' WHERE version=1"); err != nil {
		t.Fatal(err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}
	if db, err := Open(context.Background(), path); err == nil {
		db.Close()
		t.Fatal("tampered applied migration was accepted")
	} else if !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("unexpected error: %v", err)
	}
}
