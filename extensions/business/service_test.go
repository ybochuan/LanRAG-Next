package business

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/lifecycle"
	"github.com/ybochuan/LanRAG-Next/extensions/provenance"
	"github.com/ybochuan/LanRAG-Next/extensions/store"
)

func fixture(t *testing.T) (*Service, *store.DB, *time.Time) {
	t.Helper()
	db, e := store.OpenDurable(context.Background(), filepath.Join(t.TempDir(), "business.db"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { db.Close() })
	now := time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)
	return New(db, nil, func() time.Time { return now }), db, &now
}
func cmd(action, op string) Command {
	return Command{Scope: provenance.Scope{TenantID: 7, KnowledgeBaseID: "kb"}, ActorID: "7:owner", Action: action, OperationID: op}
}
func TestFeedbackCoolingConcurrencyAndAtomicAudit(t *testing.T) {
	s, db, clock := fixture(t)
	ctx := context.Background()
	c := cmd("feedback_create", "collect")
	c.Payload = Payload{Text: "Answer with the current effective version", Topic: "effective-date", Risk: "high"}
	f, e := s.Execute(ctx, c)
	if e != nil || f.Status != "pending_review" {
		t.Fatal(f, e)
	}
	c = cmd("feedback_approve", "approve")
	c.ID = f.ID
	c.ExpectedRevision = f.Revision
	approved, e := s.Execute(ctx, c)
	if e != nil || approved.Status != "approved" {
		t.Fatal(approved, e)
	}
	c = cmd("feedback_activate", "early")
	c.ID = f.ID
	c.ExpectedRevision = approved.Revision
	c.Payload.SecondConfirmation = true
	if _, e = s.Execute(ctx, c); !errors.Is(e, ErrConflict) {
		t.Fatal("early activation", e)
	}
	*clock = clock.Add(73 * time.Hour)
	c.OperationID = "missing-confirm"
	c.Payload.SecondConfirmation = false
	if _, e = s.Execute(ctx, c); e == nil {
		t.Fatal("high risk requires second confirmation")
	}
	c.Payload.SecondConfirmation = true
	var wins atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			q := c
			q.OperationID = "activate-" + string(rune('a'+i))
			if _, e := s.Execute(ctx, q); e == nil {
				wins.Add(1)
			}
		}(i)
	}
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatal("concurrent winners", wins.Load())
	}
	if e = db.Write(ctx, func(tx *sql.Tx) error {
		_, e := tx.Exec(`CREATE TRIGGER fail_business_audit BEFORE INSERT ON ext_business_audit BEGIN SELECT RAISE(ABORT,'injected'); END`)
		return e
	}); e != nil {
		t.Fatal(e)
	}
	c = cmd("feedback_create", "audit-failure")
	c.Payload = Payload{Text: "must roll back", Topic: "other", Risk: "low"}
	if _, e = s.Execute(ctx, c); e == nil {
		t.Fatal("audit failure ignored")
	}
	if e = db.View(ctx, func(tx *sql.Tx) error {
		var n int
		e := tx.QueryRow(`SELECT count(*) FROM ext_feedback_staging WHERE topic='other'`).Scan(&n)
		if n != 0 {
			t.Fatal("uncommitted governance fact", n)
		}
		return e
	}); e != nil {
		t.Fatal(e)
	}
}
func TestFeedbackIdempotencyAndGuidanceIsolation(t *testing.T) {
	s, db, _ := fixture(t)
	ctx := context.Background()
	c := cmd("guidance_seed", "seed")
	c.Payload = Payload{Text: "Use dated evidence", Topic: "date"}
	r, e := s.Execute(ctx, c)
	if e != nil || r.Status != "active" {
		t.Fatal(r, e)
	}
	again, e := s.Execute(ctx, c)
	if e != nil || again != r {
		t.Fatal(again, e)
	}
	rows, e := s.List(ctx, Command{Scope: c.Scope, Action: "guidance_list"})
	if e != nil || len(rows) != 1 || rows[0]["text"] != c.Payload.Text {
		t.Fatal("manager cannot inspect guidance", rows, e)
	}
	c.Payload.Text = "different"
	if _, e = s.Execute(ctx, c); !errors.Is(e, ErrConflict) {
		t.Fatal("conflicting replay", e)
	}
	e = db.View(ctx, func(tx *sql.Tx) error {
		var n int
		if e := tx.QueryRow(`SELECT count(*) FROM ext_feedback_guidances`).Scan(&n); e != nil {
			return e
		}
		if n != 1 {
			t.Fatal(n)
		}
		if e := tx.QueryRow(`SELECT count(*) FROM ext_lifecycle_current`).Scan(&n); e != nil {
			return e
		}
		if n != 0 {
			t.Fatal("feedback changed facts")
		}
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
}

type remoteFixture struct {
	mu      sync.Mutex
	creates int
	target  RemoteDocument
	unknown bool
}

func (r *remoteFixture) Reconcile(_ context.Context, _ Publication) (RemoteDocument, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.target, nil
}
func (r *remoteFixture) Create(_ context.Context, p Publication) (RemoteDocument, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.creates++
	r.target = RemoteDocument{ID: "target", ContentHash: p.ContentHash, Indexed: true}
	if r.unknown {
		r.unknown = false
		return RemoteDocument{}, errors.New("remote committed but ACK lost")
	}
	return r.target, nil
}
func TestPublishUnknownResultReconcilesWithoutRecreating(t *testing.T) {
	s, db, _ := fixture(t)
	ctx := context.Background()
	scope := provenance.Scope{TenantID: 7, KnowledgeBaseID: "kb"}
	// Registry-less fixture supplies immutable versions directly; production requires Registry.
	if _, e := lifecycle.NewStore(db).Append(ctx, lifecycle.Event{EventID: "source-active", TenantID: 7, KnowledgeBaseID: "kb", DocumentID: "source", ToState: lifecycle.Active, Reason: "fixture", OccurredAt: time.Now()}); e != nil {
		t.Fatal(e)
	}
	seedVersions(t, db, scope)
	s.verify = func(_ context.Context, _ provenance.Scope, doc string) (string, error) { return "version-" + doc, nil }
	g := cmd("guidance_seed", "seed")
	g.Payload = Payload{Text: "Use replacement text", Topic: "topic"}
	gr, e := s.Execute(ctx, g)
	if e != nil {
		t.Fatal(e)
	}
	c := cmd("revision_create", "propose")
	c.Payload = Payload{DocumentID: "source", Text: "replacement", GuidanceIDs: []string{gr.ID}}
	p, e := s.Execute(ctx, c)
	if e != nil {
		t.Fatal(e)
	}
	c = cmd("revision_approve", "review")
	c.ID = p.ID
	c.ExpectedRevision = p.Revision
	p, e = s.Execute(ctx, c)
	if e != nil {
		t.Fatal(e)
	}
	remote := &remoteFixture{unknown: true}
	s.WithRemote(remote)
	verifiedSource := s.verify
	s.verify = func(context.Context, provenance.Scope, string) (string, error) { return "version-changed", nil }
	if _, e = s.Publish(ctx, scope, p.ID); e == nil || remote.creates != 0 {
		t.Fatal("changed source published", e, remote.creates)
	}
	s.verify = verifiedSource
	if _, e = s.Publish(ctx, scope, p.ID); e == nil {
		t.Fatal("unknown result should be reported")
	}
	var filename string
	if e = db.View(ctx, func(tx *sql.Tx) error {
		var seq int
		var name string
		return tx.QueryRow(`PRAGMA database_list`).Scan(&seq, &name, &filename)
	}); e != nil {
		t.Fatal(e)
	}
	if e = db.Close(); e != nil {
		t.Fatal(e)
	}
	db, e = store.OpenDurable(ctx, filename)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { db.Close() })
	s = New(db, nil, s.now).WithRemote(remote)
	s.verify = verifiedSource
	if e = db.Write(ctx, func(tx *sql.Tx) error {
		_, e := tx.Exec(`CREATE TRIGGER fail_publish_audit BEFORE INSERT ON ext_business_audit WHEN NEW.action='revision_published' BEGIN SELECT RAISE(ABORT,'injected'); END`)
		return e
	}); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Publish(ctx, scope, p.ID); e == nil {
		t.Fatal("final audit failure ignored")
	}
	if e = db.View(ctx, func(tx *sql.Tx) error {
		state, _, e := store.ReadLifecycleCurrentTx(ctx, tx, 7, "kb", "source")
		if state != "active" || e != nil {
			t.Fatal("partial source retirement", state, e)
		}
		var edges int
		if e = tx.QueryRow(`SELECT count(*) FROM ext_lineage_edge WHERE edge_type='supersedes'`).Scan(&edges); e != nil {
			return e
		}
		if edges != 0 {
			t.Fatal("partial critical edge")
		}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	if e = db.Write(ctx, func(tx *sql.Tx) error { _, e := tx.Exec(`DROP TRIGGER fail_publish_audit`); return e }); e != nil {
		t.Fatal(e)
	}
	out, e := s.Publish(ctx, scope, p.ID)
	if e != nil || out.Status != "published" || remote.creates != 1 {
		t.Fatal(out, e, remote.creates)
	}
	if e = db.View(ctx, func(tx *sql.Tx) error {
		var n int
		e := tx.QueryRow(`SELECT count(*) FROM ext_lineage_edge WHERE edge_type='supersedes'`).Scan(&n)
		if n != 1 {
			t.Fatal(n)
		}
		state, _, err := store.ReadLifecycleCurrentTx(ctx, tx, 7, "kb", "source")
		if err != nil || state != "superseded" {
			t.Fatal("published original remains retrievable", state, err)
		}
		return e
	}); e != nil {
		t.Fatal(e)
	}
}
func seedVersions(t *testing.T, db *store.DB, s provenance.Scope) {
	t.Helper()
	e := db.Write(context.Background(), func(tx *sql.Tx) error {
		for _, id := range []string{"version-source", "version-target"} {
			if _, e := tx.Exec(`INSERT INTO ext_lineage_node VALUES(?, '7','kb','document_version',?,'2026-11-02T00:00:00Z')`, id, id); e != nil {
				return e
			}
			doc := id[len("version-"):]
			if _, e := tx.Exec(`INSERT INTO ext_document_version_family VALUES(?, '7','kb',?,'',?,?,1,'2026-11-02T00:00:00Z','2026-11-02T00:00:00Z')`, "family-"+doc, doc, id, id); e != nil {
				return e
			}
		}
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = lifecycle.NewStore(db).Append(context.Background(), lifecycle.Event{EventID: "target-active", TenantID: 7, KnowledgeBaseID: "kb", DocumentID: "target", ToState: lifecycle.Active, Reason: "fixture", OccurredAt: time.Now()}); e != nil {
		t.Fatal(e)
	}
}
