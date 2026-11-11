package retrieval

import (
	"context"
	"database/sql"
	"errors"
	"github.com/ybochuan/LanRAG-Next/extensions/contract"
	"github.com/ybochuan/LanRAG-Next/extensions/lifecycle"
	"github.com/ybochuan/LanRAG-Next/extensions/store"
	"github.com/ybochuan/LanRAG-Next/extensions/weknora"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestLegacyInconsistentCurrentCannotPermitAfterReopen(t *testing.T) {
	for _, corruption := range []string{"lagging_head", "mismatched_state", "missing_projection"} {
		t.Run(corruption, func(t *testing.T) {
			mismatch := corruption == "mismatched_state"
			ctx := context.Background()
			path := filepath.Join(t.TempDir(), "ext.db")
			db, err := store.OpenDurable(ctx, path)
			if err != nil {
				t.Fatal(err)
			}
			initial := lifecycle.Active
			if mismatch || corruption == "missing_projection" {
				initial = lifecycle.Stale
			}
			_, err = lifecycle.NewStore(db).Append(ctx, lifecycle.Event{EventID: "old", TenantID: 7, KnowledgeBaseID: "kb-a", DocumentID: "legacy", ToState: initial, Reason: "fixture", OccurredAt: time.Now()})
			if err != nil {
				t.Fatal(err)
			}
			err = db.Write(ctx, func(tx *sql.Tx) error {
				if corruption == "missing_projection" {
					_, e := tx.Exec("DELETE FROM ext_lifecycle_current")
					return e
				}
				if mismatch {
					// Simulate the pre-007 on-disk state only in this owned temporary fixture.
					if _, e := tx.Exec("DROP TRIGGER ext_lifecycle_current_update_matches_event"); e != nil {
						return e
					}
					_, e := tx.Exec("UPDATE ext_lifecycle_current SET state='active'")
					return e
				}
				_, e := tx.Exec(`INSERT INTO ext_lifecycle_events(event_id,tenant_id,knowledge_base_id,document_id,from_state,to_state,reason,occurred_at,stage,attempt,payload_json) VALUES('new','7','kb-a','legacy','active','stale','fixture','2026-11-01T00:00:00Z','fixture',0,'{}')`)
				return e
			})
			if err != nil {
				t.Fatal(err)
			}
			db.Close()
			db, err = store.OpenDurable(ctx, path)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			if _, e := lifecycle.NewMachine(db, time.Now).Snapshot(ctx, lifecycle.Scope{TenantID: 7, KnowledgeBaseID: "kb-a", DocumentID: "legacy"}); !errors.Is(e, store.ErrLifecycleInconsistent) {
				t.Fatalf("corrupt current must not look absent or initialize: %v", e)
			}
			if _, e := lifecycle.NewStore(db).Current(ctx, lifecycle.Scope{TenantID: 7, KnowledgeBaseID: "kb-a", DocumentID: "legacy"}); !errors.Is(e, store.ErrLifecycleInconsistent) {
				t.Fatalf("corrupt current exposed: %v", e)
			}
			policy, err := NewPolicy(db, 7, []string{"kb-a"})
			if err != nil {
				t.Fatal(err)
			}
			req := contract.BeforeRetrieveRequest{Context: contract.Context{RequestID: "r", SessionID: "s", Mode: "rag", TenantID: 7, KnowledgeBaseIDs: []string{"kb-a"}, KnowledgeIDs: []string{"legacy"}}}
			excluded, err := policy.ExcludedDocumentIDs(ctx, req)
			if err != nil || !reflect.DeepEqual(excluded, []string{"legacy"}) {
				t.Fatalf("inconsistent legacy permitted: %v %v", excluded, err)
			}
		})
	}
}

func TestPolicyOnlyAllowsKnownActiveWithinScope(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "ext.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, state := range []string{"active", "stale", "archived", "superseded", "failed", "pending", "scheduled"} {
		_, err = lifecycle.NewStore(db).Append(ctx, lifecycle.Event{EventID: state, TenantID: 7, KnowledgeBaseID: "kb-a", DocumentID: state, ToState: lifecycle.State(state), Reason: "test", OccurredAt: time.Now()})
		if err != nil {
			t.Fatal(err)
		}
	}
	// A different tenant's active record must not authorize this tenant's missing doc.
	_, err = lifecycle.NewStore(db).Append(ctx, lifecycle.Event{EventID: "foreign", TenantID: 8, KnowledgeBaseID: "kb-a", DocumentID: "foreign", ToState: "active", Reason: "test", OccurredAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	p, err := NewPolicy(db, 7, []string{"kb-a"})
	if err != nil {
		t.Fatal(err)
	}
	req := contract.BeforeRetrieveRequest{Context: contract.Context{RequestID: "r", SessionID: "s", Mode: "rag", TenantID: 7, KnowledgeBaseIDs: []string{"kb-a"}, KnowledgeIDs: []string{"active", "stale", "archived", "superseded", "failed", "pending", "scheduled", "missing", "foreign"}}}
	got, err := p.ExcludedDocumentIDs(ctx, req)
	if err != nil || !reflect.DeepEqual(got, req.KnowledgeIDs[1:]) {
		t.Fatalf("%v %v", got, err)
	}
	req.TenantID = 8
	if _, err = p.ExcludedDocumentIDs(ctx, req); err == nil {
		t.Fatal("tenant escape")
	}
	req.TenantID = 7
	req.KnowledgeBaseIDs = []string{"kb-b"}
	if _, err = p.ExcludedDocumentIDs(ctx, req); err == nil {
		t.Fatal("KB escape")
	}
	req.KnowledgeBaseIDs = []string{"kb-a"}
	db.Close()
	if _, err = p.ExcludedDocumentIDs(ctx, req); err == nil {
		t.Fatal("DB unavailable permitted")
	}
}

func TestPolicyRejectsDatesAndDisabledDocumentsBeforeSweep(t *testing.T) {
	ctx := t.Context()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "ext.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ids := []string{"valid", "expired", "future", "bad-date", "disabled"}
	for _, id := range ids {
		if _, err := lifecycle.NewStore(db).Append(ctx, lifecycle.Event{EventID: id, TenantID: 7, KnowledgeBaseID: "kb-a", DocumentID: id, ToState: lifecycle.Active, Reason: "fixture", OccurredAt: time.Now()}); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.Exec(`INSERT INTO ext_document_annotations VALUES
		('7','kb-a','expired',1,NULL,'2000-01-01T00:00:00Z',NULL,'{}'),
		('7','kb-a','future',1,'2099-01-01T00:00:00Z',NULL,NULL,'{}'),
		('7','kb-a','bad-date',1,NULL,'malformed',NULL,'{}')`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	docs := make([]weknora.AttachmentDocument, 0, len(ids))
	for _, id := range ids {
		docs = append(docs, weknora.AttachmentDocument{ID: id, KnowledgeBaseID: "kb-a", TenantID: 7, EnableStatus: "enabled"})
	}
	docs[len(docs)-1].EnableStatus = "disabled"
	p, _ := NewPolicy(db, 7, []string{"kb-a"})
	p.WithAttachments(attachmentSourceFixture{docs: docs})
	for _, mode := range []string{"rag", "agent"} {
		req := contract.BeforeRetrieveRequest{Context: contract.Context{RequestID: "r", SessionID: "s", Mode: mode, TenantID: 7, KnowledgeBaseIDs: []string{"kb-a"}, KnowledgeIDs: ids}}
		if got, err := p.ExcludedDocumentIDs(ctx, req); err != nil || !reflect.DeepEqual(got, ids[1:]) {
			t.Fatalf("%s excluded %v, %v; want %v", mode, got, err, ids[1:])
		}
	}
}
