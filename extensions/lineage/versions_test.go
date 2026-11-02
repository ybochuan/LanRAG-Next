package lineage

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/lifecycle"
	"github.com/ybochuan/LanRAG-Next/extensions/provenance"
	"github.com/ybochuan/LanRAG-Next/extensions/store"
)

type fixtureReader struct {
	mu       sync.Mutex
	snapshot Snapshot
	err      error
	calls    int
}

func (r *fixtureReader) Capture(context.Context, provenance.Scope, string) (Snapshot, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	return r.snapshot, r.err
}
func fixtureSnapshot(t *testing.T, body string) Snapshot {
	t.Helper()
	fields := map[string]string{}
	for _, name := range headerNames {
		fields[name] = sha([]byte("null"))
	}
	fields["parse_status"] = sha([]byte(`"completed"`))
	s := Snapshot{Scheme: Scheme, TenantID: 7, KnowledgeBaseID: "kb", DocumentID: "doc", HeaderFields: fields, Chunks: []ChunkLeaf{{ID: "chunk", ContentHash: sha([]byte(body)), FieldsHash: strings.Repeat("a", 64)}}}
	s.HeaderHash = headerDigest(fields)
	s.RootHash = rootDigest(s)
	return s
}
func registryFixture(t *testing.T) (*Registry, *store.DB, *fixtureReader, string) {
	t.Helper()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "ext.db")
	db, e := store.OpenDurable(ctx, path)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { db.Close() })
	_, e = lifecycle.NewStore(db).Append(ctx, lifecycle.Event{EventID: "event", TenantID: 7, KnowledgeBaseID: "kb", DocumentID: "doc", ToState: lifecycle.Active, Reason: "fixture", OccurredAt: time.Now()})
	if e != nil {
		t.Fatal(e)
	}
	r := &fixtureReader{snapshot: fixtureSnapshot(t, "first")}
	return NewRegistry(db, r), db, r, path
}

var fixtureScope = provenance.Scope{TenantID: 7, KnowledgeBaseID: "kb"}

func TestObservedVersionsAreImmutableAndNotSupersedes(t *testing.T) {
	r, db, reader, _ := registryFixture(t)
	ctx := context.Background()
	v, e := r.Refresh(ctx, fixtureScope, "doc")
	if e != nil {
		t.Fatal(e)
	}
	same, e := r.Refresh(ctx, fixtureScope, "doc")
	if e != nil || same.ID != v.ID || v.Sequence != 1 {
		t.Fatal(v, same, e)
	}
	reader.snapshot = fixtureSnapshot(t, "edited")
	next, e := r.Refresh(ctx, fixtureScope, "doc")
	if e != nil || next.ID == v.ID || next.FamilyID != v.FamilyID || next.Sequence != 2 {
		t.Fatal(next, e)
	}
	if e = db.View(ctx, func(tx *sql.Tx) error {
		var state string
		var edges, versions int
		if e := tx.QueryRow("SELECT state FROM ext_lifecycle_current").Scan(&state); e != nil {
			return e
		}
		tx.QueryRow("SELECT COUNT(*) FROM ext_document_version").Scan(&versions)
		tx.QueryRow("SELECT COUNT(*) FROM ext_lineage_edge").Scan(&edges)
		if state != "active" || edges != 0 || versions != 2 {
			t.Fatalf("state=%s edges=%d versions=%d", state, edges, versions)
		}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
}

func TestSixteenRefreshesAcrossIndependentWriters(t *testing.T) {
	r, db, reader, path := registryFixture(t)
	ctx := context.Background()
	other, e := store.OpenDurable(ctx, path)
	if e != nil {
		t.Fatal(e)
	}
	defer other.Close()
	rs := []*Registry{r, NewRegistry(other, reader)}
	var wg sync.WaitGroup
	out := make(chan Version, 16)
	errs := make(chan error, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) { defer wg.Done(); v, e := rs[i%2].Refresh(ctx, fixtureScope, "doc"); out <- v; errs <- e }(i)
	}
	wg.Wait()
	close(out)
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	first := ""
	for v := range out {
		if first == "" {
			first = v.ID
		}
		if v.ID != first || v.Sequence != 1 {
			t.Fatal(v)
		}
	}
	db.View(ctx, func(tx *sql.Tx) error {
		var count int
		tx.QueryRow("SELECT COUNT(*) FROM ext_lineage_audit").Scan(&count)
		if count != 1 {
			t.Fatal(count)
		}
		return nil
	})
}

func TestEveryVersionFactRollsBack(t *testing.T) {
	for _, table := range []string{"ext_lineage_node", "ext_document_version_family", "ext_document_version", "ext_source_snapshot_evidence", "ext_lineage_audit"} {
		t.Run(table, func(t *testing.T) {
			r, db, _, _ := registryFixture(t)
			ctx := context.Background()
			e := db.Write(ctx, func(tx *sql.Tx) error {
				_, e := tx.Exec(fmt.Sprintf("CREATE TRIGGER abort_version BEFORE INSERT ON %s BEGIN SELECT RAISE(ABORT,'fixture'); END", table))
				return e
			})
			if e != nil {
				t.Fatal(e)
			}
			v, e := r.Refresh(ctx, fixtureScope, "doc")
			if e == nil || v.ID != "" {
				t.Fatal(v, e)
			}
			db.View(ctx, func(tx *sql.Tx) error {
				for _, name := range []string{"ext_lineage_node", "ext_document_version_family", "ext_document_version", "ext_source_snapshot_evidence", "ext_lineage_audit"} {
					var n int
					tx.QueryRow("SELECT COUNT(*) FROM " + name).Scan(&n)
					if n != 0 {
						t.Fatal(name, n)
					}
				}
				return nil
			})
			db.Write(ctx, func(tx *sql.Tx) error { _, e := tx.Exec("DROP TRIGGER abort_version"); return e })
			if _, e = r.Refresh(ctx, fixtureScope, "doc"); e != nil {
				t.Fatal(e)
			}
		})
	}
}

func TestLiveAuthorityRejectsUnknownEvidenceAndNativeChanges(t *testing.T) {
	r, db, reader, _ := registryFixture(t)
	ctx := context.Background()
	a := NewAuthority(r, nil)
	v, e := r.Refresh(ctx, fixtureScope, "doc")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = a.CurrentSource(ctx, fixtureScope, "doc", "forged"); e == nil {
		t.Fatal("forged evidence accepted")
	}
	p, e := a.CurrentSource(ctx, fixtureScope, "doc", v.ID)
	if e != nil || p.VersionID != v.ID || p.ObservationSequence != 1 {
		t.Fatal(p, e)
	}
	refs := []provenance.SourceRef{{DocumentID: "doc", VersionID: v.ID}}
	if e = a.VerifyCurrent(ctx, fixtureScope, refs); e != nil {
		t.Fatal(e)
	}
	reader.snapshot = fixtureSnapshot(t, "native changed without observation timer")
	if e = a.VerifyCurrent(ctx, fixtureScope, refs); e == nil {
		t.Fatal("old native root admitted")
	}
	if e = db.View(ctx, func(tx *sql.Tx) error { return a.VerifyCurrentTx(ctx, tx, fixtureScope, refs) }); e == nil {
		t.Fatal("stale authority head admitted")
	}
	if _, e = a.SealedCopy(ctx, fixtureScope, "copy", provenance.Binding{}, "model-complete"); e == nil {
		t.Fatal("no sealed authority accepted copy")
	}
}

func TestSnapshotIntegrityAndScopeFailClosed(t *testing.T) {
	r, _, reader, _ := registryFixture(t)
	reader.snapshot.RootHash = strings.Repeat("f", 64)
	if _, e := r.Refresh(context.Background(), fixtureScope, "doc"); e == nil {
		t.Fatal("bad root accepted")
	}
	reader.snapshot = fixtureSnapshot(t, "ok")
	if _, e := r.Refresh(context.Background(), provenance.Scope{TenantID: 8, KnowledgeBaseID: "kb"}, "doc"); e == nil {
		t.Fatal("cross scope accepted")
	}
}

type sealedFixture struct{ version string }

func (s sealedFixture) SealedCopy(_ context.Context, scope provenance.Scope, copy string, b provenance.Binding, id string) (provenance.CopyProof, error) {
	return provenance.CopyProof{Scope: scope, CopyID: copy, Binding: b, EvidenceID: id, EvidenceHash: strings.Repeat("b", 64), RunID: "run", Sources: []provenance.SourceRef{{DocumentID: "doc", VersionID: s.version}}}, nil
}

func TestRealAuthorityFeedsLedgerAndRechecksNativeOnEveryRead(t *testing.T) {
	r, db, reader, _ := registryFixture(t)
	ctx := context.Background()
	v, e := r.Refresh(ctx, fixtureScope, "doc")
	if e != nil {
		t.Fatal(e)
	}
	a := NewAuthority(r, sealedFixture{v.ID})
	l := provenance.New(db, a)
	if _, e = l.RegisterSource(ctx, provenance.SourceRequest{Scope: fixtureScope, OperationID: "register", DocumentID: "doc", EvidenceID: v.ID}); e != nil {
		t.Fatal(e)
	}
	b := provenance.Binding{UserID: "user", SessionID: "session", ResourceID: "resource", ContentVersion: "v1", SHA256: strings.Repeat("a", 64)}
	if _, e = l.Save(ctx, provenance.SaveRequest{Scope: fixtureScope, OperationID: "save", CopyID: "copy", Binding: b, EvidenceID: "sealed"}); e != nil {
		t.Fatal(e)
	}
	request := provenance.ReadRequest{Scope: fixtureScope, CopyID: "copy", Revision: 1, Binding: b}
	if d, e := l.CheckRead(ctx, request); e != nil || !d.Allowed {
		t.Fatal(d, e)
	}
	reader.mu.Lock()
	before := reader.calls
	reader.mu.Unlock()
	wrong := request
	wrong.Binding.UserID = "other-user"
	if d, _ := l.CheckRead(ctx, wrong); d.Allowed {
		t.Fatal("wrong binding allowed")
	}
	reader.mu.Lock()
	after := reader.calls
	reader.mu.Unlock()
	if before != after {
		t.Fatal("wrong binding triggered native source query")
	}
	reader.snapshot = fixtureSnapshot(t, "native edit before next timer")
	if d, _ := l.CheckRead(ctx, request); d.Allowed {
		t.Fatal("copy allowed after native root changed")
	}
}

func TestFreshEvidenceCanReplaceButNeverPromoteLegacySource(t *testing.T) {
	r, db, _, _ := registryFixture(t)
	ctx := context.Background()
	e := db.Write(ctx, func(tx *sql.Tx) error {
		if _, e := tx.Exec("INSERT INTO ext_provenance_sources VALUES('7','kb','doc','legacy',?,10,'event',?)", strings.Repeat("a", 64), strings.Repeat("b", 64)); e != nil {
			return e
		}
		_, e := tx.Exec("INSERT INTO ext_provenance_source_heads VALUES('7','kb','doc',1,'legacy',10)")
		return e
	})
	if e != nil {
		t.Fatal(e)
	}
	v, e := r.Refresh(ctx, fixtureScope, "doc")
	if e != nil {
		t.Fatal(e)
	}
	a := NewAuthority(r, nil)
	if e = a.VerifyCurrent(ctx, fixtureScope, []provenance.SourceRef{{DocumentID: "doc", VersionID: "legacy"}}); e == nil {
		t.Fatal("legacy fact was promoted")
	}
	l := provenance.New(db, a)
	if _, e = l.RegisterSource(ctx, provenance.SourceRequest{Scope: fixtureScope, OperationID: "fresh", DocumentID: "doc", EvidenceID: v.ID, ExpectedRevision: 1}); e != nil {
		t.Fatal("fresh verified evidence cannot replace legacy head", e)
	}
}
