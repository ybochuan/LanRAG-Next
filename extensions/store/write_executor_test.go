package store

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestWriteExecutorSerializesOneThousandRealSQLiteWrites(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	if err := db.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.Exec("CREATE TABLE ext_feedback_probe (id INTEGER PRIMARY KEY, payload TEXT NOT NULL)")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	var workers sync.WaitGroup
	results := make(chan error, 1000)
	for i := 0; i < 1000; i++ {
		workers.Add(1)
		go func(id int) {
			defer workers.Done()
			results <- db.Write(ctx, func(tx *sql.Tx) error {
				_, err := tx.Exec("INSERT INTO ext_feedback_probe VALUES (?, ?)", id, "feedback")
				return err
			})
		}(i)
	}
	workers.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := db.View(ctx, func(tx *sql.Tx) error {
		var count, unique int
		if err := tx.QueryRow("SELECT COUNT(*), COUNT(DISTINCT id) FROM ext_feedback_probe").Scan(&count, &unique); err != nil {
			return err
		}
		if count != 1000 || unique != 1000 {
			t.Fatalf("count=%d unique=%d", count, unique)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestWriteExecutorRollsBackErrorPanicAndCanceledWork(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	if err := db.Write(ctx, func(tx *sql.Tx) error { _, err := tx.Exec("CREATE TABLE ext_rollback_probe(id INTEGER)"); return err }); err != nil {
		t.Fatal(err)
	}
	sentinel := errors.New("reject transaction")
	if err := db.Write(ctx, func(tx *sql.Tx) error {
		if _, err := tx.Exec("INSERT INTO ext_rollback_probe VALUES(1)"); err != nil {
			return err
		}
		return sentinel
	}); !errors.Is(err, sentinel) {
		t.Fatalf("error=%v", err)
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Error("callback panic was swallowed")
			}
		}()
		_ = db.Write(ctx, func(tx *sql.Tx) error { _, _ = tx.Exec("INSERT INTO ext_rollback_probe VALUES(2)"); panic("abort") })
	}()
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if err := db.Write(canceled, func(*sql.Tx) error { t.Error("canceled callback executed"); return nil }); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel error=%v", err)
	}
	during, abort := context.WithCancel(ctx)
	if err := db.Write(during, func(tx *sql.Tx) error {
		if _, err := tx.Exec("INSERT INTO ext_rollback_probe VALUES(4)"); err != nil {
			return err
		}
		abort()
		// Let database/sql's cancellation goroutine roll the transaction back first.
		time.Sleep(10 * time.Millisecond)
		return nil
	}); !errors.Is(err, context.Canceled) {
		t.Fatalf("active cancellation=%v", err)
	}
	if err := db.View(ctx, func(tx *sql.Tx) error {
		var count int
		if err := tx.QueryRow("SELECT COUNT(*) FROM ext_rollback_probe").Scan(&count); err != nil {
			return err
		}
		if count != 0 {
			t.Fatalf("rolled-back rows persisted: %d", count)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Write(ctx, func(tx *sql.Tx) error { _, err := tx.Exec("INSERT INTO ext_rollback_probe VALUES(3)"); return err }); err != nil {
		t.Fatal(err)
	}
}

func TestCanceledQueueWaitDoesNotExecuteCallbackAndCloseRejectsNewWork(t *testing.T) {
	db := openTestDB(t)
	entered, release := make(chan struct{}), make(chan struct{})
	defer close(release)
	first := make(chan error, 1)
	go func() {
		first <- db.Write(context.Background(), func(*sql.Tx) error { close(entered); <-release; return nil })
	}()
	<-entered
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := db.Write(ctx, func(*sql.Tx) error { t.Error("canceled queued callback ran"); return nil }); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("queued cancellation=%v", err)
	}
	release <- struct{}{}
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := db.Write(context.Background(), func(*sql.Tx) error { return nil }); !errors.Is(err, ErrClosed) {
		t.Fatalf("closed writer=%v", err)
	}
	if err := db.View(context.Background(), func(*sql.Tx) error { return nil }); !errors.Is(err, ErrClosed) {
		t.Fatalf("closed reader=%v", err)
	}
}
