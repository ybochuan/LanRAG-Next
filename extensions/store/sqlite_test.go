package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"modernc.org/sqlite"
)

func openTestDB(t *testing.T) *DB {
	t.Helper()
	db, err := Open(context.Background(), filepath.Join(t.TempDir(), "agentrag_ext.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	return db
}

func checkPragmas(tx *sql.Tx, reader bool) error {
	var journal string
	if err := tx.QueryRow("PRAGMA journal_mode").Scan(&journal); err != nil || journal != "wal" {
		return fmt.Errorf("journal mode=%q err=%v", journal, err)
	}
	for name, want := range map[string]int{
		"synchronous": 1, "busy_timeout": 5000, "foreign_keys": 1,
		"cache_size": -64000, "wal_autocheckpoint": 1000,
	} {
		var got int
		if err := tx.QueryRow("PRAGMA " + name).Scan(&got); err != nil || got != want {
			return fmt.Errorf("pragma %s=%d want=%d err=%v", name, got, want, err)
		}
	}
	var queryOnly int
	if err := tx.QueryRow("PRAGMA query_only").Scan(&queryOnly); err != nil || (reader && queryOnly != 1) {
		return fmt.Errorf("query_only=%d err=%v", queryOnly, err)
	}
	return nil
}

func TestSQLiteConfiguresWriterAndEveryPhysicalReader(t *testing.T) {
	db := openTestDB(t)
	if err := db.Write(context.Background(), func(tx *sql.Tx) error {
		if err := checkPragmas(tx, false); err != nil {
			return err
		}
		_, err := tx.Exec("CREATE TABLE ext_readonly_probe (id INTEGER PRIMARY KEY)")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ready := make(chan struct{}, 3)
	release := make(chan struct{})
	defer close(release)
	results := make(chan error, 3)
	for i := 0; i < 3; i++ {
		go func() {
			results <- db.View(ctx, func(tx *sql.Tx) error {
				if err := checkPragmas(tx, true); err != nil {
					return err
				}
				if _, err := tx.Exec("INSERT INTO ext_readonly_probe VALUES(1)"); err == nil {
					t.Error("reader accepted a write")
				}
				ready <- struct{}{}
				select {
				case <-release:
				case <-ctx.Done():
				}
				return nil
			})
		}()
	}
	for i := 0; i < 3; i++ {
		select {
		case <-ready:
		case err := <-results:
			t.Fatalf("reader initialization: %v", err)
		case <-ctx.Done():
			t.Fatal("physical readers failed to open concurrently")
		}
	}
	for i := 0; i < 3; i++ {
		release <- struct{}{}
	}
	for i := 0; i < 3; i++ {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
}

func TestSQLiteRejectsExistingForeignDatabaseWithoutChangingIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "existing.db")
	foreign, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := foreign.Exec("CREATE TABLE knowledges (id TEXT); INSERT INTO knowledges VALUES ('original')"); err != nil {
		t.Fatal(err)
	}
	if err := foreign.Close(); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	db, err := Open(context.Background(), path)
	if err == nil {
		db.Close()
		t.Fatal("accepted a database containing upstream tables")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if sha256.Sum256(before) != sha256.Sum256(after) {
		t.Fatal("foreign database changed during rejection")
	}
}

func TestWriterTakesImmediateLockWhileReadersRemainAvailable(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "资料 #&=库.db")
	db, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Write(ctx, func(tx *sql.Tx) error { _, err := tx.Exec("CREATE TABLE ext_lock_probe(id INTEGER)"); return err }); err != nil {
		t.Fatal(err)
	}
	other, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	if err := db.Write(ctx, func(*sql.Tx) error {
		// No DML has run on the reserved writer yet: only BEGIN IMMEDIATE can
		// prevent this separate connection from obtaining the write lock.
		_, err := other.Exec("INSERT INTO ext_lock_probe VALUES(1)")
		var sqliteErr *sqlite.Error
		if !errors.As(err, &sqliteErr) || sqliteErr.Code() != 5 {
			return fmt.Errorf("expected SQLITE_BUSY on competing writer, got %v", err)
		}
		return db.View(ctx, func(tx *sql.Tx) error {
			var count int
			if err := tx.QueryRow("SELECT COUNT(*) FROM ext_lock_probe").Scan(&count); err != nil {
				return err
			}
			if count != 0 {
				return errors.New("competing write unexpectedly committed")
			}
			return nil
		})
	}); err != nil {
		t.Fatal(err)
	}
}
