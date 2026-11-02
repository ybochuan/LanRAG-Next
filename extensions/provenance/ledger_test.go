package provenance

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/lifecycle"
	"github.com/ybochuan/LanRAG-Next/extensions/store"
)

type testAuthority struct {
	mu      sync.Mutex
	sources map[string]SourceProof
	copies  map[string]CopyProof
}

func (a *testAuthority) CurrentSource(_ context.Context, _ Scope, _ string, id string) (SourceProof, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	p, ok := a.sources[id]
	if !ok {
		return p, ErrEvidence
	}
	return p, nil
}
func (a *testAuthority) SealedCopy(_ context.Context, _ Scope, _ string, _ Binding, id string) (CopyProof, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	p, ok := a.copies[id]
	if !ok {
		return p, ErrEvidence
	}
	return p, nil
}

var testScope = Scope{TenantID: 7, KnowledgeBaseID: "kb"}
var testBinding = Binding{UserID: "user", SessionID: "session", ResourceID: "resource", ContentVersion: "content-v1", SHA256: strings.Repeat("a", 64)}

func setup(t *testing.T) (*Ledger, *testAuthority) {
	t.Helper()
	db, err := store.OpenDurable(context.Background(), filepath.Join(t.TempDir(), "ext.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	a := &testAuthority{sources: map[string]SourceProof{}, copies: map[string]CopyProof{}}
	return New(db, a), a
}
func seedSource(t *testing.T, l *Ledger, a *testAuthority, doc string) {
	t.Helper()
	event := "event-" + doc
	_, err := lifecycle.NewStore(l.db).Append(context.Background(), lifecycle.Event{EventID: event, TenantID: 7, KnowledgeBaseID: "kb", DocumentID: doc, ToState: lifecycle.Active, Reason: "fixture", OccurredAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	a.sources[doc] = SourceProof{Scope: testScope, DocumentID: doc, VersionID: "v1", ContentHash: strings.Repeat("b", 64), ObservationSequence: 1, LifecycleEventID: event, EvidenceID: doc, EvidenceHash: strings.Repeat("c", 64)}
	if _, err = l.RegisterSource(context.Background(), SourceRequest{Scope: testScope, OperationID: "source-" + doc, DocumentID: doc, EvidenceID: doc}); err != nil {
		t.Fatal(err)
	}
}
func saveReady(t *testing.T, l *Ledger, a *testAuthority, id string, sources []SourceRef, parents []ParentRef) Receipt {
	t.Helper()
	a.copies[id] = CopyProof{Scope: testScope, CopyID: id, Binding: testBinding, EvidenceID: id, EvidenceHash: strings.Repeat("d", 64), RunID: "run", Sources: sources, Parents: parents}
	r, err := l.Save(context.Background(), SaveRequest{Scope: testScope, CopyID: id, OperationID: "save-" + id, Binding: testBinding, EvidenceID: id})
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func request(id string, revision int64) ReadRequest {
	return ReadRequest{Scope: testScope, CopyID: id, Revision: revision, Binding: testBinding}
}
func requireRead(t *testing.T, l *Ledger, r ReadRequest, want bool) {
	t.Helper()
	d, err := l.CheckRead(context.Background(), r)
	if err != nil || d.Allowed != want {
		t.Fatalf("decision=%+v err=%v", d, err)
	}
}

func TestDraftAndMissingAuthorityNeverAdmit(t *testing.T) {
	l, _ := setup(t)
	l.authority = nil
	r, err := l.Save(context.Background(), SaveRequest{Scope: testScope, CopyID: "draft", OperationID: "draft", Binding: testBinding})
	if err != nil || r.Status != "pending" {
		t.Fatalf("%+v %v", r, err)
	}
	requireRead(t, l, request("draft", 1), false)
	_, err = l.Save(context.Background(), SaveRequest{Scope: testScope, CopyID: "draft", OperationID: "publish", Binding: testBinding, ExpectedRevision: 1, EvidenceID: "fake"})
	if !errors.Is(err, ErrEvidence) {
		t.Fatal(err)
	}
}
func TestCurrentStateVersionsAndBinding(t *testing.T) {
	l, a := setup(t)
	seedSource(t, l, a, "doc")
	saveReady(t, l, a, "copy", []SourceRef{{DocumentID: "doc", VersionID: "v1"}}, nil)
	requireRead(t, l, request("copy", 1), true)
	for _, field := range []string{"tenant", "kb", "user", "session", "hash", "version", "revision"} {
		r := request("copy", 1)
		switch field {
		case "tenant":
			r.Scope.TenantID = 8
		case "kb":
			r.Scope.KnowledgeBaseID = "other"
		case "user":
			r.Binding.UserID = "other"
		case "session":
			r.Binding.SessionID = "other"
		case "hash":
			r.Binding.SHA256 = strings.Repeat("e", 64)
		case "version":
			r.Binding.ContentVersion = "other"
		case "revision":
			r.Revision = 2
		}
		requireRead(t, l, r, false)
	}
	proof := a.sources["doc"]
	proof.VersionID = "v2"
	proof.ObservationSequence = 2
	proof.ContentHash = strings.Repeat("f", 64)
	proof.EvidenceID = "doc-v2"
	a.sources[proof.EvidenceID] = proof
	if _, err := l.RegisterSource(context.Background(), SourceRequest{Scope: testScope, OperationID: "source-v2", DocumentID: "doc", EvidenceID: proof.EvidenceID, ExpectedRevision: 1}); err != nil {
		t.Fatal(err)
	}
	requireRead(t, l, request("copy", 1), false)
	if _, err := l.RegisterSource(context.Background(), SourceRequest{Scope: testScope, OperationID: "rollback", DocumentID: "doc", EvidenceID: "doc", ExpectedRevision: 2}); !errors.Is(err, ErrConflict) {
		t.Fatalf("version rollback=%v", err)
	}
}
func TestStaleExpiryAndInheritance(t *testing.T) {
	l, a := setup(t)
	seedSource(t, l, a, "one")
	seedSource(t, l, a, "two")
	saveReady(t, l, a, "parent", []SourceRef{{"one", "v1"}}, nil)
	saveReady(t, l, a, "child", []SourceRef{{"two", "v1"}}, []ParentRef{{CopyID: "parent", Revision: 1}})
	requireRead(t, l, request("child", 1), true)
	m, err := l.Snapshot(context.Background(), testScope, "child")
	if err != nil || len(m.Sources) != 2 {
		t.Fatalf("%+v %v", m, err)
	}
	_, err = lifecycle.NewStore(l.db).Append(context.Background(), lifecycle.Event{EventID: "stale-one", TenantID: 7, KnowledgeBaseID: "kb", DocumentID: "one", FromState: lifecycle.Active, ToState: lifecycle.Stale, Reason: "fixture", OccurredAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	requireRead(t, l, request("child", 1), false)
}
func TestIdempotencyCASAndNoSourceShrinking(t *testing.T) {
	l, a := setup(t)
	seedSource(t, l, a, "one")
	seedSource(t, l, a, "two")
	saveReady(t, l, a, "copy", []SourceRef{{"one", "v1"}, {"two", "v1"}}, nil)
	original := SaveRequest{Scope: testScope, CopyID: "copy", OperationID: "save-copy", Binding: testBinding, EvidenceID: "copy"}
	if _, err := l.Save(context.Background(), original); err != nil {
		t.Fatal(err)
	}
	bad := original
	bad.Binding.UserID = "other"
	if _, err := l.Save(context.Background(), bad); !errors.Is(err, ErrConflict) {
		t.Fatalf("reuse=%v", err)
	}
	a.copies["small"] = CopyProof{Scope: testScope, CopyID: "copy", Binding: testBinding, EvidenceID: "small", EvidenceHash: strings.Repeat("d", 64), RunID: "run", Sources: []SourceRef{{"one", "v1"}}}
	bad = original
	bad.OperationID = "shrink"
	bad.EvidenceID = "small"
	bad.ExpectedRevision = 1
	if _, err := l.Save(context.Background(), bad); !errors.Is(err, ErrConflict) {
		t.Fatalf("shrink=%v", err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 16)
	for n := 0; n < 16; n++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := l.Save(context.Background(), original); results <- err }()
	}
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}
}
