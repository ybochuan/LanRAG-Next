package lineage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/dependencies"
	"github.com/ybochuan/LanRAG-Next/extensions/lifecycle"
	"github.com/ybochuan/LanRAG-Next/extensions/provenance"
	"github.com/ybochuan/LanRAG-Next/extensions/store"
)

const renditionBody = `{"schema_version":"2.0","pages":[]}`

func renditionFixture(t *testing.T) (*Registry, *store.DB, *fixtureReader, string, string) {
	t.Helper()
	reg, db, reader, _ := registryFixture(t)
	snapshot := reader.snapshot
	snapshot.HeaderFields["enable_status"] = sha([]byte(`"enabled"`))
	snapshot.HeaderHash = headerDigest(snapshot.HeaderFields)
	snapshot.RootHash = rootDigest(snapshot)
	reader.snapshot = snapshot
	id, key, source := sha([]byte("identity")), sha([]byte("job")), sha([]byte("original PDF"))
	journal, err := json.Marshal(map[string]any{
		"Version": 1, "Files": map[string]bool{"input": true, "zip": false},
		"Uploads": map[string]any{
			"upload":               map[string]any{"Hash": source, "BackendFile": "input", "Verified": true},
			"cache-hit-incomplete": map[string]any{"Hash": source, "BackendFile": "input", "Verified": false},
		},
		"Hashes": map[string]string{"input": source}, "Documents": map[string]string{"input": "doc"},
		"Jobs": map[string]bool{"job": true}, "JobKeys": map[string]string{"job": key},
		"OutputJobKeys": map[string]string{"zip": key},
		"OutputDocuments": map[string]any{"zip": map[string]any{
			"tenant_id": 7, "knowledge_base_id": "kb", "doc_id": "doc", "source_file_id": "input", "mineru_version": "4.0.1",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Write(t.Context(), func(tx *sql.Tx) error {
		now := time.Now().UTC().Format(time.RFC3339Nano)
		if _, err := tx.Exec(`INSERT INTO ext_parser_documents VALUES(?,7,'kb','doc',?,?)`, id, journal, now); err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO ext_parser_jobs VALUES(?,?,201,?)`, id, key, []byte(`{"job_id":"job","status":"processing"}`)); err != nil {
			return err
		}
		_, err := tx.Exec(`INSERT INTO ext_parser_artifacts VALUES(?,?,7,'kb','doc','input','middle_json','2.0','4.0.1',?,?,?)`, id, key, sha([]byte(renditionBody)), []byte(renditionBody), now)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return reg, db, reader, id, source
}

func TestRenditionBindsProvenBytesOnceAndRejectsChangedSource(t *testing.T) {
	reg, _, reader, id, source := renditionFixture(t)
	ctx := t.Context()
	foreign := provenance.Scope{TenantID: 7, KnowledgeBaseID: "other"}
	wrong := NewRenditions(reg, func(context.Context, string) (string, error) { return sha([]byte("replacement")), nil })
	if _, err := wrong.Bind(ctx, fixtureScope, "doc", id); !errors.Is(err, ErrConflict) {
		t.Fatalf("wrong original: %v", err)
	}
	service := NewRenditions(reg, func(context.Context, string) (string, error) { return source, nil })
	if _, err := service.Bind(ctx, foreign, "doc", id); !errors.Is(err, ErrEvidence) {
		t.Fatalf("foreign KB: %v", err)
	}
	var first, second RenditionBinding
	var firstErr, secondErr error
	var wg sync.WaitGroup
	wg.Go(func() { first, firstErr = service.Bind(ctx, fixtureScope, "doc", id) })
	wg.Go(func() { second, secondErr = service.Bind(ctx, fixtureScope, "doc", id) })
	wg.Wait()
	if firstErr != nil || secondErr != nil || first != second || first.VersionID == "" || first.NodeID == "" {
		t.Fatal(first, second, firstErr, secondErr)
	}
	// A lost response must be replayable without creating a second binding.
	retry, err := service.Bind(ctx, fixtureScope, "doc", id)
	if err != nil || retry != first {
		t.Fatalf("lost-response retry changed the binding: %+v %v", retry, err)
	}
	got, body, err := service.Read(ctx, fixtureScope, "doc", id)
	if err != nil || got != first || string(body) != renditionBody {
		t.Fatal(got, err)
	}
	if _, _, err := service.Read(ctx, foreign, "doc", id); !errors.Is(err, ErrEvidence) {
		t.Fatalf("foreign KB read released bytes: %v", err)
	}
	reader.mu.Lock()
	reader.snapshot.Chunks[0].ContentHash = sha([]byte("changed"))
	reader.snapshot.RootHash = rootDigest(reader.snapshot)
	reader.mu.Unlock()
	if _, err := service.Bind(ctx, fixtureScope, "doc", id); !errors.Is(err, ErrConflict) {
		t.Fatalf("old conversion moved to new version: %v", err)
	}
	if _, _, err := service.Read(ctx, fixtureScope, "doc", id); !errors.Is(err, ErrConflict) {
		t.Fatalf("read old conversion after original version changed: %v", err)
	}
}

func TestRenditionReadDeniesInactiveOriginalAndCorruptBytes(t *testing.T) {
	reg, db, _, id, source := renditionFixture(t)
	ctx := t.Context()
	service := NewRenditions(reg, func(context.Context, string) (string, error) { return source, nil })
	if _, err := service.Bind(ctx, fixtureScope, "doc", id); err != nil {
		t.Fatal(err)
	}
	if err := db.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.Exec(`UPDATE ext_parser_artifacts SET middle_json='{"schema_version":"2.0","pages":[1]}' WHERE identity_key=?`, id)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.Read(ctx, fixtureScope, "doc", id); !errors.Is(err, ErrEvidence) {
		t.Fatalf("corrupt bytes released: %v", err)
	}
	if err := db.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.Exec(`UPDATE ext_parser_artifacts SET middle_json=? WHERE identity_key=?`, renditionBody, id)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := lifecycle.NewStore(db).Append(ctx, lifecycle.Event{EventID: "expired", TenantID: 7, KnowledgeBaseID: "kb", DocumentID: "doc", FromState: lifecycle.Active, ToState: lifecycle.Stale, Reason: "fixture", OccurredAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.Read(ctx, fixtureScope, "doc", id); !errors.Is(err, ErrConflict) {
		t.Fatalf("inactive original released: %v", err)
	}
}

type renditionSnapshotReader func(context.Context, provenance.Scope, string) (Snapshot, error)

func (r renditionSnapshotReader) Capture(ctx context.Context, scope provenance.Scope, doc string) (Snapshot, error) {
	return r(ctx, scope, doc)
}

func TestRenditionGraphRootsByDocumentID(t *testing.T) {
	reg, _, _, id, source := renditionFixture(t)
	ctx := t.Context()
	if _, err := NewRenditions(reg, func(context.Context, string) (string, error) { return source, nil }).Bind(ctx, fixtureScope, "doc", id); err != nil {
		t.Fatal(err)
	}
	graph, err := reg.Graph(ctx, fixtureScope, "renditions", "doc")
	if err != nil {
		t.Fatal(err)
	}
	if graph.RootID != "doc" || len(graph.Nodes) != 2 || len(graph.Edges) != 1 {
		t.Fatalf("document root: %+v", graph)
	}
	if graph.Edges[0].Type != "rendition_of" || graph.Edges[0].From == "" || graph.Edges[0].To == "" {
		t.Fatalf("document root edge: %+v", graph.Edges[0])
	}
}

func TestRenditionRefreshesNativeSupplementParent(t *testing.T) {
	reg, db, child, id, source := renditionFixture(t)
	ctx := t.Context()
	if _, err := lifecycle.NewStore(db).Append(ctx, lifecycle.Event{EventID: "parent-active", TenantID: 7, KnowledgeBaseID: "kb", DocumentID: "parent", ToState: lifecycle.Active, Reason: "fixture", OccurredAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	parent := "original policy"
	reg.reader = renditionSnapshotReader(func(ctx context.Context, s provenance.Scope, doc string) (Snapshot, error) {
		if doc != "parent" {
			return child.Capture(ctx, s, doc)
		}
		snap := fixtureSnapshot(t, parent)
		snap.DocumentID = doc
		snap.RootHash = rootDigest(snap)
		return snap, nil
	})
	if _, err := NewSupplements(db, reg, time.Now).Put(ctx, SupplementCommand{
		Scope:   dependencies.Scope{TenantID: 7, KnowledgeBaseID: "kb", DocumentID: "doc"},
		ActorID: "actor", OperationID: "fixture-parent", Parents: []string{"parent"},
	}); err != nil {
		t.Fatal(err)
	}
	service := NewRenditions(reg, func(context.Context, string) (string, error) { return source, nil })
	if _, err := service.Bind(ctx, fixtureScope, "doc", id); err != nil {
		t.Fatal(err)
	}
	parent = "edited native parent"
	if _, _, err := service.Read(ctx, fixtureScope, "doc", id); !errors.Is(err, ErrConflict) {
		t.Fatalf("unobserved parent change should block bytes: %v", err)
	}
}
