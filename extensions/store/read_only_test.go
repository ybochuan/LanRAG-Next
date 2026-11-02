package store

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

func TestReadOnlyNeverCreatesMigratesOrWrites(t *testing.T) {
	ctx := context.Background()
	missing := filepath.Join(t.TempDir(), "missing.db")
	if db, err := OpenReadOnly(ctx, missing); err == nil {
		db.Close()
		t.Fatal("missing DB opened")
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Fatal("missing DB created")
	}
	path := filepath.Join(t.TempDir(), "existing.db")
	old, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = old.Exec("CREATE TABLE ext_schema_migrations(version INTEGER PRIMARY KEY);INSERT INTO ext_schema_migrations VALUES(4)"); err != nil {
		t.Fatal(err)
	}
	old.Close()
	reader, err := OpenReadOnly(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	var version int
	if err = reader.QueryRow("SELECT MAX(version) FROM ext_schema_migrations").Scan(&version); err != nil || version != 4 {
		t.Fatalf("migrated %d %v", version, err)
	}
	if _, err = reader.Exec("CREATE TABLE ext_forbidden(id INTEGER)"); err == nil {
		t.Fatal("reader wrote database")
	}
}

func TestReadOnlyRejectsNativeDB(t *testing.T) {
	path := filepath.Join(t.TempDir(), "native.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("CREATE TABLE users(id INTEGER)"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if reader, err := OpenReadOnly(context.Background(), path); err == nil {
		reader.Close()
		t.Fatal("native DB accepted")
	}
}
