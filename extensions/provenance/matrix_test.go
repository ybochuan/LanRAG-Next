package provenance

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"strings"
	"testing"
)

// runFactCounts snapshots every table one run operation can touch, so a
// failed transaction can be proven to have written nothing at all.
func runFactCounts(t *testing.T, r *Runs) []int {
	t.Helper()
	tables := []string{"ext_provenance_runs", "ext_provenance_run_audit", "ext_provenance_steps", "ext_provenance_inputs", "ext_provenance_input_intents", "ext_provenance_producers"}
	result := make([]int, len(tables))
	err := r.db.View(context.Background(), func(tx *sql.Tx) error {
		for i, table := range tables {
			if err := tx.QueryRow("SELECT count(*) FROM " + table).Scan(&result[i]); err != nil {
				return err
			}
		}
		rows, err := tx.Query("PRAGMA foreign_key_check")
		if err != nil {
			return err
		}
		defer rows.Close()
		if rows.Next() {
			return errors.New("broken foreign key")
		}
		return rows.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

// injectFault installs an aborting trigger on one table; dropping it restores
// normal behaviour so the same operation can be retried afterwards.
func injectFault(t *testing.T, r *Runs, table, name string) {
	t.Helper()
	// The same fact table is inserted by one operation and updated by another
	// (a run is inserted when it begins, updated when it seals), so a fault that
	// only covered INSERT would silently skip the update path.
	if err := r.db.Write(context.Background(), func(tx *sql.Tx) error {
		for _, event := range []string{"INSERT", "UPDATE"} {
			if _, err := tx.Exec("CREATE TRIGGER " + name + "_" + strings.ToLower(event) + " BEFORE " + event + " ON " + table + " BEGIN SELECT RAISE(ABORT,'injected'); END"); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func dropFault(t *testing.T, r *Runs, name string) {
	t.Helper()
	if err := r.db.Write(context.Background(), func(tx *sql.Tx) error {
		for _, event := range []string{"insert", "update"} {
			if _, err := tx.Exec("DROP TRIGGER IF EXISTS " + name + "_" + event); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// TestSealSQLFailureLeavesTheOpenRunUntouched covers every table the seal
// transaction writes. A failed seal must not advance the run head, must not
// append audit, and must stay retryable once the fault is gone.
func TestSealSQLFailureLeavesTheOpenRunUntouched(t *testing.T) {
	for _, table := range []string{"ext_provenance_runs", "ext_provenance_run_audit"} {
		t.Run(table, func(t *testing.T) {
			r, h := runFixture(t)
			finishModel(t, r, h, true)
			ctx := context.Background()
			before, err := r.Inspect(ctx, h)
			if err != nil {
				t.Fatal(err)
			}
			counts := runFactCounts(t, r)
			injectFault(t, r, table, "seal_fail")
			if _, err := r.Finish(ctx, h, FinishRequest{OperationID: "seal", ExpectedRevision: before.Revision}); err == nil {
				t.Fatal("faulted seal reported success")
			}
			after, err := r.Inspect(ctx, h)
			if err != nil {
				t.Fatal(err)
			}
			if after.State != "open" || after.Revision != before.Revision || !reflect.DeepEqual(counts, runFactCounts(t, r)) {
				t.Fatalf("partial seal: before=%s/%d after=%s/%d", before.State, before.Revision, after.State, after.Revision)
			}
			dropFault(t, r, "seal_fail")
			sealed, err := r.Finish(ctx, h, FinishRequest{OperationID: "seal", ExpectedRevision: before.Revision})
			if err != nil || sealed.State != "sealed" {
				t.Fatalf("retry after fault failed: %v %+v", err, sealed)
			}
		})
	}
}

// TestProducerSQLFailureLeavesNoReservationOrCommit covers the reserve and
// commit paths over every table they write. A failed reserve must leave no
// producer row; a failed commit must leave the reservation uncommitted so the
// caller can retry the acknowledgement without repeating the write.
func TestProducerSQLFailureLeavesNoReservationOrCommit(t *testing.T) {
	ctx := context.Background()
	reserveTables := []string{"ext_provenance_runs", "ext_provenance_run_audit", "ext_provenance_producers"}
	for _, table := range reserveTables {
		t.Run("reserve-"+table, func(t *testing.T) {
			r, h := runFixture(t)
			finishModel(t, r, h, true)
			step, err := r.BeginStep(ctx, h, StepRequest{OperationID: "write", Kind: "write"})
			if err != nil {
				t.Fatal(err)
			}
			before, err := r.Inspect(ctx, h)
			if err != nil {
				t.Fatal(err)
			}
			counts := runFactCounts(t, r)
			injectFault(t, r, table, "producer_fail")
			request := ProducerRequest{OperationID: "reserve", StepID: step.StepID, CopyID: "copy", Path: "reports/result.md", Binding: testBinding}
			if _, err := r.ReserveProducer(ctx, h, request); err == nil {
				t.Fatal("faulted reservation reported success")
			}
			after, err := r.Inspect(ctx, h)
			if err != nil {
				t.Fatal(err)
			}
			if after.Revision != before.Revision || !reflect.DeepEqual(counts, runFactCounts(t, r)) {
				t.Fatalf("partial reservation: before=%d after=%d", before.Revision, after.Revision)
			}
			dropFault(t, r, "producer_fail")
			if _, err := r.ReserveProducer(ctx, h, request); err != nil {
				t.Fatalf("retry after fault failed: %v", err)
			}
		})
	}
	commitTables := []string{"ext_provenance_runs", "ext_provenance_run_audit", "ext_provenance_producers"}
	for _, table := range commitTables {
		t.Run("commit-"+table, func(t *testing.T) {
			r, h := runFixture(t)
			finishModel(t, r, h, true)
			step, err := r.BeginStep(ctx, h, StepRequest{OperationID: "write", Kind: "write"})
			if err != nil {
				t.Fatal(err)
			}
			reserved, err := r.ReserveProducer(ctx, h, ProducerRequest{OperationID: "reserve", StepID: step.StepID, CopyID: "copy", Path: "reports/result.md", Binding: testBinding})
			if err != nil {
				t.Fatal(err)
			}
			before, err := r.Inspect(ctx, h)
			if err != nil {
				t.Fatal(err)
			}
			injectFault(t, r, table, "commit_fail")
			commit := ProducerResult{OperationID: "commit", ProducerID: reserved.ProducerID, StepID: step.StepID, Binding: testBinding}
			if err := r.CommitProducer(ctx, h, commit); err == nil {
				t.Fatal("faulted commit reported success")
			}
			after, err := r.Inspect(ctx, h)
			if err != nil {
				t.Fatal(err)
			}
			if after.Revision != before.Revision {
				t.Fatalf("failed commit advanced the head: %d -> %d", before.Revision, after.Revision)
			}
			var producerState string
			if err := r.db.View(ctx, func(tx *sql.Tx) error {
				return tx.QueryRow("SELECT state FROM ext_provenance_producers WHERE producer_id=?", reserved.ProducerID).Scan(&producerState)
			}); err != nil {
				t.Fatal(err)
			}
			if producerState != "reserved" {
				t.Fatalf("producer state after failed commit = %q, want reserved", producerState)
			}
			dropFault(t, r, "commit_fail")
			if err := r.CommitProducer(ctx, h, commit); err != nil {
				t.Fatalf("retry commit failed: %v", err)
			}
		})
	}
}
