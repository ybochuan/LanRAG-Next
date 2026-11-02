package store

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

func TestDurableWriterUsesFullSync(t *testing.T) {
	db, err := OpenDurable(context.Background(), filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if !db.FullSync() {
		t.Fatal("durable flag missing")
	}
	if err := db.Write(context.Background(), func(tx *sql.Tx) error {
		var sync int
		if err := tx.QueryRow("PRAGMA synchronous").Scan(&sync); err != nil {
			return err
		}
		if sync != 2 {
			t.Errorf("synchronous=%d want FULL(2)", sync)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestDurableWriterPreservesNormalInConfiguredPath(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "NORMAL", "state.db")
	old, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	_ = old.Close()
	if err := os.MkdirAll(filepath.Join(root, "FULL"), 0700); err != nil {
		t.Fatal(err)
	}
	db, err := OpenDurable(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Write(context.Background(), func(tx *sql.Tx) error {
		var sync int
		if err := tx.QueryRow("PRAGMA synchronous").Scan(&sync); err != nil {
			return err
		}
		if sync != 2 {
			t.Errorf("writer synchronous=%d want FULL", sync)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "FULL", "state.db")); !os.IsNotExist(err) {
		t.Error("configured NORMAL path rewritten to FULL")
	}
}
