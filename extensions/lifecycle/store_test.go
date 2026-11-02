package lifecycle

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/store"
)

func database(t *testing.T) *store.DB {
	t.Helper()
	db, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "agentrag_ext.db"))
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

func initialEvent() Event {
	return Event{EventID: "event-1", TenantID: 7, KnowledgeBaseID: "kb-a", DocumentID: "doc-a",
		ToState: "pending_review", Reason: "accepted", OccurredAt: time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC),
		SourceEventID: "source-1", Stage: "index_completed", Attempt: 1}
}

func counts(t *testing.T, db *store.DB) (result [3]int) {
	t.Helper()
	if err := db.View(context.Background(), func(tx *sql.Tx) error {
		for i, table := range []string{"ext_lifecycle_events", "ext_lifecycle_current", "ext_projection_outbox"} {
			if err := tx.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&result[i]); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestAppendIsAtomicIdempotentAndReplayable(t *testing.T) {
	db := database(t)
	s := NewStore(db)
	ctx := context.Background()
	first := initialEvent()
	if inserted, err := s.Append(ctx, first); err != nil || !inserted {
		t.Fatalf("append=%v %v", inserted, err)
	}
	second := first
	second.EventID, second.SourceEventID = "event-2", "source-2"
	second.FromState, second.ToState = first.ToState, "active"
	second.OccurredAt = first.OccurredAt.Add(time.Second)
	if inserted, err := s.Append(ctx, second); err != nil || !inserted {
		t.Fatalf("append=%v %v", inserted, err)
	}
	if inserted, err := s.Append(ctx, first); err != nil || inserted {
		t.Fatalf("retry after newer event=%v %v", inserted, err)
	}
	if got := counts(t, db); got != [3]int{2, 1, 2} {
		t.Fatalf("counts=%v", got)
	}
	scope := Scope{TenantID: 7, KnowledgeBaseID: "kb-a", DocumentID: "doc-a"}
	events, err := s.Events(ctx, scope)
	if err != nil || len(events) != 2 {
		t.Fatalf("events=%v err=%v", events, err)
	}
	var replay State
	for _, event := range events {
		if replay != event.FromState {
			t.Fatal("event log cannot reconstruct current")
		}
		replay = event.ToState
	}
	current, err := s.Current(ctx, scope)
	if err != nil || current.State != replay || current.LastEventID != second.EventID {
		t.Fatalf("current=%+v err=%v", current, err)
	}
	scope.TenantID = 8
	if _, err := s.Current(ctx, scope); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("cross tenant=%v", err)
	}
}

func TestAppendRejectsConflictingKeysAndStaleFromState(t *testing.T) {
	db := database(t)
	s := NewStore(db)
	ctx := context.Background()
	first := initialEvent()
	if _, err := s.Append(ctx, first); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*Event){
		func(e *Event) { e.Reason = "different payload" },
		func(e *Event) { e.EventID = "different-id" },
		func(e *Event) { e.TenantID = 8 },
	} {
		event := first
		change(&event)
		if _, err := s.Append(ctx, event); !errors.Is(err, ErrEventConflict) {
			t.Fatalf("conflict=%v", err)
		}
	}
	stale := first
	stale.EventID, stale.SourceEventID = "new-event", "new-source"
	stale.FromState, stale.ToState = "draft", "active"
	if _, err := s.Append(ctx, stale); !errors.Is(err, ErrStateConflict) {
		t.Fatalf("stale from=%v", err)
	}
	if got := counts(t, db); got != [3]int{1, 1, 1} {
		t.Fatalf("counts=%v", got)
	}
}

func TestAppendRollsBackFailureAtEverySQLStage(t *testing.T) {
	for _, table := range []string{"ext_lifecycle_events", "ext_lifecycle_current", "ext_projection_outbox"} {
		t.Run(table, func(t *testing.T) {
			db := database(t)
			ctx := context.Background()
			if err := db.Write(ctx, func(tx *sql.Tx) error {
				_, err := tx.Exec("CREATE TRIGGER ext_abort BEFORE INSERT ON " + table + " BEGIN SELECT RAISE(ABORT, 'injected failure'); END")
				return err
			}); err != nil {
				t.Fatal(err)
			}
			if inserted, err := NewStore(db).Append(ctx, initialEvent()); err == nil || inserted {
				t.Fatalf("failed transaction acknowledged: %v %v", inserted, err)
			}
			if got := counts(t, db); got != [3]int{} {
				t.Fatalf("partial commit=%v", got)
			}
		})
	}
}

func TestCurrentForeignKeyRejectsMissingAndCrossScopeEvent(t *testing.T) {
	db := database(t)
	ctx := context.Background()
	if _, err := NewStore(db).Append(ctx, initialEvent()); err != nil {
		t.Fatal(err)
	}
	for _, eventID := range []string{"missing", "event-1"} {
		if err := db.Write(ctx, func(tx *sql.Tx) error {
			// State matches event-1 so the schema7 guard stays silent and the
			// composite foreign key is what rejects the out-of-scope reference.
			_, err := tx.Exec("INSERT INTO ext_lifecycle_current(tenant_id,knowledge_base_id,document_id,state,last_event_id) VALUES('7','kb-a','other-doc','pending_review',?)", eventID)
			return err
		}); err == nil {
			t.Fatalf("accepted foreign event %s", eventID)
		}
	}
}

func TestCurrentGuardRejectsStateThatNoEventProduced(t *testing.T) {
	db := database(t)
	ctx := context.Background()
	if _, err := NewStore(db).Append(ctx, initialEvent()); err != nil {
		t.Fatal(err)
	}
	// The corruption this guard exists for: a hand-written UPDATE moves the
	// projection without any event. Without the guard every foreign key still
	// held, so the document read as stale while its log never left pending_review.
	if err := db.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.Exec("UPDATE ext_lifecycle_current SET state='stale' WHERE document_id='doc-a'")
		return err
	}); err == nil {
		t.Fatal("guard accepted an update to a state no event produced")
	}
	var state string
	if err := db.View(ctx, func(tx *sql.Tx) error {
		return tx.QueryRow("SELECT state FROM ext_lifecycle_current WHERE document_id='doc-a'").Scan(&state)
	}); err != nil {
		t.Fatal(err)
	}
	if state != "pending_review" {
		t.Fatalf("state=%s survived a rejected write", state)
	}
	// The insert path: event-b exists in scope, but the row claims another state.
	if err := db.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.Exec(`INSERT INTO ext_lifecycle_events(event_id,tenant_id,knowledge_base_id,document_id,from_state,to_state,reason,occurred_at,stage,attempt,payload_json)
			VALUES('event-b','7','kb-a','doc-b','','active','index_completed','2026-10-29T00:00:00Z','',0,'{}')`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.Exec("INSERT INTO ext_lifecycle_current(tenant_id,knowledge_base_id,document_id,state,last_event_id) VALUES('7','kb-a','doc-b','stale','event-b')")
		return err
	}); err == nil {
		t.Fatal("guard accepted an insert with a state no event produced")
	}
	// A matching projection is still accepted, so the guard does not block rebuilds.
	if err := db.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.Exec("INSERT INTO ext_lifecycle_current(tenant_id,knowledge_base_id,document_id,state,last_event_id) VALUES('7','kb-a','doc-b','active','event-b')")
		return err
	}); err != nil {
		t.Fatalf("guard rejected a consistent rebuild: %v", err)
	}
}

func TestCurrentGuardRejectsPointerBehindLog(t *testing.T) {
	db := database(t)
	ctx := context.Background()
	s := NewStore(db)
	first := initialEvent()
	if inserted, err := s.Append(ctx, first); err != nil || !inserted {
		t.Fatalf("append=%v %v", inserted, err)
	}
	second := first
	second.EventID, second.SourceEventID = "event-2", "source-2"
	second.FromState, second.ToState = first.ToState, "active"
	second.OccurredAt = first.OccurredAt.Add(time.Second)
	if inserted, err := s.Append(ctx, second); err != nil || !inserted {
		t.Fatalf("append=%v %v", inserted, err)
	}
	// The corruption a matching state cannot catch: point the projection back at
	// the first event. Its to_state is pending_review, so the state guard stays
	// silent while the projection lags a committed transition to active.
	if err := db.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.Exec("UPDATE ext_lifecycle_current SET state=?,last_event_id=? WHERE document_id='doc-a'", first.ToState, first.EventID)
		return err
	}); err == nil {
		t.Fatal("guard accepted a pointer behind the latest committed event")
	}
	var state, last string
	if err := db.View(ctx, func(tx *sql.Tx) error {
		return tx.QueryRow("SELECT state,last_event_id FROM ext_lifecycle_current WHERE document_id='doc-a'").Scan(&state, &last)
	}); err != nil {
		t.Fatal(err)
	}
	if State(state) != second.ToState || last != second.EventID {
		t.Fatalf("projection=%s/%s survived a rejected write", state, last)
	}
	// A rebuild that does point at the true head is still accepted.
	if err := db.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.Exec("UPDATE ext_lifecycle_current SET state=?,last_event_id=? WHERE document_id='doc-a'", second.ToState, second.EventID)
		return err
	}); err != nil {
		t.Fatalf("guard rejected a projection at the latest event: %v", err)
	}
}
