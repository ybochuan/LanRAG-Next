package business

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/contract"
	"github.com/ybochuan/LanRAG-Next/extensions/lifecycle"
	"github.com/ybochuan/LanRAG-Next/extensions/provenance"
	"github.com/ybochuan/LanRAG-Next/extensions/retrieval"
	"github.com/ybochuan/LanRAG-Next/extensions/store"
)

func TestSplitRejectsMarkdownNativeWouldRewrite(t *testing.T) {
	for _, invalid := range []SplitPartInput{
		{Title: "API onload= example", Text: "# Example"},
		{Title: "Fenced code", Text: "~~~html\nonerror = handler\n~~~"},
		{Title: "Raw script", Text: "<script>alert(1)</script>"},
	} {
		_, err := normalizeSplitParts([]SplitPartInput{invalid, {Title: "Other", Text: "# Other"}})
		if !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v", invalid.Title, err)
		}
	}
}

func setupSplit(t *testing.T) (*Service, *store.DB, *time.Time, Command, Result, []string) {
	t.Helper()
	s, db, clock := fixture(t)
	ctx := t.Context()
	c := cmd("split_create", "split")
	seedVersions(t, db, c.Scope)
	s.verify = func(_ context.Context, _ provenance.Scope, doc string) (string, error) {
		return "version-" + doc, nil
	}
	if _, err := lifecycle.NewStore(db).Append(ctx, lifecycle.Event{
		EventID: "active-source", TenantID: 7, KnowledgeBaseID: "kb", DocumentID: "source",
		ToState: lifecycle.Active, Reason: "fixture", OccurredAt: *clock,
	}); err != nil {
		t.Fatal(err)
	}
	g := cmd("guidance_seed", "split-guidance")
	g.Payload = Payload{Text: "Check policy boundaries", Topic: "split"}
	guidance, err := s.Execute(ctx, g)
	if err != nil {
		t.Fatal(err)
	}
	c.Payload = Payload{DocumentID: "source", GuidanceIDs: []string{guidance.ID},
		SplitParts: []SplitPartInput{{Title: "Part one", Text: "# First"}, {Title: "Part two", Text: "# Second"}}}
	batch, err := s.Execute(ctx, c)
	if err != nil || batch.Status != "pending_review" {
		t.Fatal(batch, err)
	}
	return s, db, clock, c, batch, splitTestMembers(t, db, batch.ID)
}

func TestSplitFirstPublicationCutsOverAndLaterFailureDoesNotReactivate(t *testing.T) {
	s, db, clock, c, batch, members := setupSplit(t)
	ctx := t.Context()
	approve := cmd("split_approve", "split-approval")
	approve.ID, approve.ExpectedRevision = batch.ID, batch.Revision
	remote := &splitFixture{documents: map[string]RemoteDocument{}, loseDraft: true}
	s.WithRemote(remote)
	if _, err := s.Publish(ctx, c.Scope, members[0]); !errors.Is(err, ErrConflict) {
		t.Fatalf("unapproved member publish = %v", err)
	}
	approved, err := s.Execute(ctx, approve)
	if err != nil || approved.Status != "approved" {
		t.Fatal(approved, err)
	}
	if again, err := s.Execute(ctx, approve); err != nil || again != approved {
		t.Fatalf("approval retry = %+v, %v", again, err)
	}
	if _, err := s.Publish(ctx, c.Scope, members[0]); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("lost draft ACK = %v", err)
	}
	*clock = clock.Add(3 * time.Minute)
	step := func(id, want string) Result {
		t.Helper()
		out, err := s.Publish(ctx, c.Scope, id)
		if err != nil || out.Status != want {
			t.Fatalf("publish %s: %s, %v (want %s)", id, out.Status, err, want)
		}
		return out
	}
	if r := step(members[0], "publishing"); r.TargetDocumentID != "target" || remote.creates != 1 {
		t.Fatalf("lost draft was not reconciled: %+v, creates=%d", r, remote.creates)
	}
	step(members[0], "publishing")
	seedSplitTarget(t, db, "target")
	check := func(scopeIDs, want []string) {
		t.Helper()
		p, err := retrieval.NewPolicy(db, 7, []string{"kb"})
		if err != nil {
			t.Fatal(err)
		}
		blocked, err := p.ExcludedDocumentIDs(ctx, contract.BeforeRetrieveRequest{Context: contract.Context{
			RequestID: "test", SessionID: "test", Mode: "rag", TenantID: 7, KnowledgeBaseIDs: []string{"kb"},
			KnowledgeIDs: scopeIDs,
		}})
		if err != nil || !slices.Equal(blocked, want) {
			t.Fatalf("blocked = %v, %v; want %v", blocked, err, want)
		}
	}
	initial := []string{"source", "target"}
	check(initial, []string{"target"})
	if err := db.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.Exec(`CREATE TRIGGER fail_split_audit BEFORE INSERT ON ext_business_audit
			WHEN NEW.action='revision_published' BEGIN SELECT RAISE(ABORT,'injected'); END`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Publish(ctx, c.Scope, members[0]); err == nil {
		t.Fatal("failed audit committed the first cutover")
	}
	check(initial, []string{"target"})
	if err := db.Write(ctx, func(tx *sql.Tx) error { _, err := tx.Exec("DROP TRIGGER fail_split_audit"); return err }); err != nil {
		t.Fatal(err)
	}
	first := step(members[0], "published")
	check(initial, []string{"source"})
	second := step(members[1], "publishing")
	step(members[1], "publishing")
	seedSplitTarget(t, db, second.TargetDocumentID)
	remote.fail = true
	if _, err = s.Publish(ctx, c.Scope, members[1]); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("second member failure = %v", err)
	}
	all := append(initial, second.TargetDocumentID)
	check(all, []string{"source", second.TargetDocumentID})
	if again := step(members[0], "published"); again != first {
		t.Fatalf("published retry changed: %+v", again)
	}
	*clock = clock.Add(3 * time.Minute)
	remote.fail = false
	if again := step(members[1], "published"); again.TargetDocumentID != second.TargetDocumentID || remote.creates != 2 {
		t.Fatalf("second retry changed target: %+v, creates=%d", again, remote.creates)
	}
	check(all, []string{"source"})
}

func TestSplitConcurrentPublicationRetiresSourceOnlyOnce(t *testing.T) {
	s, db, _, c, batch, members := setupSplit(t)
	ctx := t.Context()
	remote := &splitFixture{documents: map[string]RemoteDocument{}}
	s.WithRemote(remote)
	ordinary := cmd("revision_create", "ordinary-competing")
	ordinary.Payload = Payload{DocumentID: "source", GuidanceIDs: c.Payload.GuidanceIDs, Text: "Ordinary revision"}
	r, err := s.Execute(ctx, ordinary)
	if err != nil {
		t.Fatal(err)
	}
	review := cmd("revision_approve", "ordinary-approval")
	review.ID, review.ExpectedRevision = r.ID, r.Revision
	if _, err := s.Execute(ctx, review); err != nil {
		t.Fatal(err)
	}
	approve := cmd("split_approve", "split-approval")
	approve.ID, approve.ExpectedRevision = batch.ID, batch.Revision
	if _, err := s.Execute(ctx, approve); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Publish(ctx, c.Scope, r.ID); !errors.Is(err, ErrConflict) {
		t.Fatalf("ordinary revision competed with the approved split: %v", err)
	}
	for _, id := range members {
		for step := 0; step < 2; step++ {
			r, err = s.Publish(ctx, c.Scope, id)
			if err != nil || r.Status != "publishing" {
				t.Fatal(r, err)
			}
		}
		seedSplitTarget(t, db, r.TargetDocumentID)
	}
	var wg sync.WaitGroup
	start := make(chan struct{})
	for _, id := range members {
		wg.Go(func() {
			<-start
			out, err := s.Publish(ctx, c.Scope, id)
			if err != nil || out.Status != "published" {
				t.Errorf("publish %s: %+v, %v", id, out, err)
			}
		})
	}
	close(start)
	wg.Wait()
	if err := db.View(ctx, func(tx *sql.Tx) error {
		var transitions, branches, families int
		err := tx.QueryRow(`SELECT
			(SELECT count(*) FROM ext_lifecycle_events WHERE document_id='source' AND to_state='superseded'),
			(SELECT count(*) FROM ext_revision_split_ancestry WHERE to_node_id='version-source'),
			(SELECT count(DISTINCT family_id) FROM ext_revision_proposals WHERE status='published' AND family_id<>'family-source')`).
			Scan(&transitions, &branches, &families)
		if err == nil && (transitions != 1 || branches != 2 || families != 2) {
			t.Errorf("transitions=%d, branches=%d, independent families=%d", transitions, branches, families)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

func seedSplitTarget(t *testing.T, db *store.DB, doc string) {
	t.Helper()
	ctx := t.Context()
	event, version, family := "active-"+doc, "version-"+doc, "family-"+doc
	at := time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)
	if doc != "target" {
		if _, err := lifecycle.NewStore(db).Append(ctx, lifecycle.Event{
			EventID: event, TenantID: 7, KnowledgeBaseID: "kb", DocumentID: doc,
			ToState: lifecycle.Active, Reason: "fixture", OccurredAt: at,
		}); err != nil {
			t.Fatal(err)
		}
	} else {
		event = "target-active"
	}
	if err := db.Write(ctx, func(tx *sql.Tx) error {
		stamp := at.Format(publicationTime)
		if doc != "target" {
			if _, err := tx.Exec(`INSERT INTO ext_lineage_node VALUES(?,'7','kb','document_version',?,?)`, version, version, stamp); err != nil {
				return err
			}
			if _, err := tx.Exec(`INSERT INTO ext_document_version_family VALUES(?,'7','kb',?,'',?,?,1,?,?)`, family, doc, version, version, stamp, stamp); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(`INSERT INTO ext_document_version VALUES(?,'7','kb',?,?,?,1,?,'','fixture',?)`,
			version, family, doc, textHash(doc), event, stamp); err != nil {
			return err
		}
		_, err := tx.Exec(`INSERT INTO ext_source_snapshot_evidence VALUES('7','kb',?,?,?)`,
			version, `{"header_fields":{"metadata":"`+textHash("target-metadata")+`"}}`, textHash("fixture"))
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

func splitTestMembers(t *testing.T, db *store.DB, batch string) []string {
	t.Helper()
	var members []string
	if err := db.View(t.Context(), func(tx *sql.Tx) error {
		rows, err := tx.Query("SELECT proposal_id FROM ext_revision_split_parts WHERE batch_id=? ORDER BY position", batch)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				return err
			}
			members = append(members, id)
		}
		return rows.Err()
	}); err != nil || len(members) != 2 {
		t.Fatalf("members=%v, error=%v", members, err)
	}
	return members
}

type splitFixture struct {
	mu        sync.Mutex
	documents map[string]RemoteDocument
	creates   int
	loseDraft bool
	fail      bool
}

func (f *splitFixture) Reconcile(_ context.Context, p Publication) (RemoteDocument, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail {
		return RemoteDocument{}, errors.New("upstream unavailable")
	}
	return f.documents[p.Key], nil
}
func (f *splitFixture) Create(context.Context, Publication) (RemoteDocument, error) {
	return RemoteDocument{}, errors.New("split must create a draft first")
}
func (f *splitFixture) ValidateSplitSource(context.Context, Publication) error { return nil }
func (f *splitFixture) CreateDraft(_ context.Context, p Publication) (RemoteDocument, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.creates++
	id := "target"
	if f.creates > 1 {
		id = "target-second"
	}
	doc := RemoteDocument{ID: id, ContentHash: p.ContentHash, Draft: true}
	f.documents[p.Key] = doc
	if f.loseDraft {
		f.loseDraft = false
		return RemoteDocument{}, errors.New("draft created, ACK lost")
	}
	return doc, nil
}
func (f *splitFixture) IndexDraft(_ context.Context, p Publication) (RemoteDocument, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	doc := RemoteDocument{ID: p.TargetDocumentID, ContentHash: p.ContentHash, Indexed: true, MetadataHash: textHash("target-metadata")}
	f.documents[p.Key] = doc
	return doc, nil
}
