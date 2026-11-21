package business

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/corrections"
	"github.com/ybochuan/LanRAG-Next/extensions/lifecycle"
	"github.com/ybochuan/LanRAG-Next/extensions/provenance"
)

func TestReviewedCorrectionPublicationReplacementAndMarkdown(t *testing.T) {
	s, db, clock := fixture(t)
	ctx := t.Context()
	scope := cmd("", "").Scope
	_, err := lifecycle.NewStore(db).Append(ctx, lifecycle.Event{EventID: "source-active", TenantID: 7, KnowledgeBaseID: "kb", DocumentID: "source", ToState: lifecycle.Active, Reason: "fixture", OccurredAt: *clock})
	if err != nil {
		t.Fatal(err)
	}
	seedVersions(t, db, scope)
	s.verify = func(_ context.Context, _ provenance.Scope, doc string) (string, error) { return "version-" + doc, nil }
	hash := strings.Repeat("a", 64)
	pin := FeedbackCitation{ClauseAnchor: ClauseAnchor{ChunkID: "chunk", ContentHash: hash}, DocumentID: "source", VersionID: "version-source", ChunkIndex: 3}
	rawPin, _ := json.Marshal([]FeedbackCitation{pin})
	if err := db.Write(ctx, func(tx *sql.Tx) error {
		if _, err := tx.Exec(`INSERT INTO ext_document_version VALUES('version-source','7','kb','family-source','source',?,1,'source-active','','fixture','2026-11-02T00:00:00Z')`, hash); err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO ext_source_snapshot_evidence VALUES('7','kb','version-source',json_object('chunks',json_array(json_object('id','chunk','content_hash',?,'chunk_index',3))),?)`, hash, hash); err != nil {
			return err
		}
		for _, id := range []string{"first", "replacement", "withdrawn", "stale-replacement", "overlap", "disjoint"} {
			if _, err := tx.Exec(`INSERT INTO ext_qa_feedback(tenant_id,knowledge_base_id,feedback_id,submitted_at,rating,reason,text,submitted_context,citation_chunks,status,revision) VALUES('7','kb',?,'2026-11-02T00:00:00Z','correction','','private-feedback','{"question":"private-question"}',?,'pending_review',1)`, id, string(rawPin)); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	approve := func(id, text, previous string, revision int64, peers ...Result) Result {
		t.Helper()
		c := cmd("feedback_approve", "approve-"+id)
		c.ID, c.ExpectedRevision = id, 1
		condition := "Reviewer count and deadline"
		switch id {
		case "withdrawn":
			condition = "An unrelated review condition"
		case "overlap":
			condition = "Deadline and required number of reviewers"
		case "disjoint":
			condition = "Appeals about archived invoices"
		}
		c.Payload = Payload{Text: text, AppliesWhen: condition, SourceClause: &pin.ClauseAnchor, SupersedesCorrectionID: previous, SupersedesCorrectionRevision: revision}
		for _, peer := range peers {
			c.Payload.ReviewedCorrectionPeers = append(c.Payload.ReviewedCorrectionPeers, corrections.Peer{ID: peer.ID, Revision: peer.Revision})
		}
		out, err := s.Execute(ctx, c)
		if err != nil {
			t.Fatal(out, err)
		}
		return out
	}
	activation := func(approved Result) Command {
		c := cmd("feedback_activate", "activate-"+approved.ID)
		c.ID, c.ExpectedRevision = approved.ID, approved.Revision
		return c
	}
	reader := corrections.New(db, func() time.Time { return *clock })
	f := corrections.Filter{DocumentID: "source", VersionID: "version-source"}
	first := approve("first", "Corrected deadline: 5 个工作日", "", 0)
	if list, err := reader.Documents(ctx, correctionScope(scope), f, nil); err != nil || list.Total != 0 {
		t.Fatal("cooling phase appeared", list, err)
	}
	overlap := approve("overlap", "Deadline is 9 working days", "", 0)
	*clock = clock.Add(49 * time.Hour)
	first, err = s.Execute(ctx, activation(first))
	if err != nil {
		t.Fatal(first, err)
	}
	withdrawn := approve("withdrawn", "Another fact <script> & [text]", "", 0, first)
	*clock = clock.Add(49 * time.Hour)
	withdrawn, err = s.Execute(ctx, activation(withdrawn))
	if err != nil {
		t.Fatal(withdrawn, err)
	}
	disjoint := approve("disjoint", "Use the appeal form", "", 0, first, withdrawn)
	*clock = clock.Add(49 * time.Hour)
	disjoint, err = s.Execute(ctx, activation(disjoint))
	if err != nil {
		t.Fatal(err)
	}
	list, err := reader.Documents(ctx, correctionScope(scope), corrections.Filter{}, nil)
	if err != nil || list.Total != 1 || len(list.Items) != 1 || list.Items[0].DocumentID != f.DocumentID || list.Items[0].VersionID != f.VersionID || list.Items[0].ActiveCount != 3 {
		t.Fatalf("three corrections did not share one version: %+v %v", list, err)
	}
	doc, err := reader.Document(ctx, correctionScope(scope), f, nil)
	if err != nil || len(doc.Items) != 3 || !doc.LastApprovedAt.Equal(doc.Items[0].ApprovedAt) || strings.Contains(doc.Markdown, "private-") || strings.Contains(doc.Markdown, "<script>") {
		t.Fatalf("unexpected published document: %+v %v", doc, err)
	}
	for i, id := range []string{"disjoint", "withdrawn", "first"} {
		if entry := doc.Items[i]; entry.ID != id || entry.Ordinal != i+1 {
			t.Fatalf("unexpected entry order: %+v", doc.Items)
		}
	}
	if sources, err := reader.Sources(ctx, correctionScope(scope), []string{"source"}, "哪项规定是 5 个工作日"); err != nil || len(sources) != 1 || sources[0].ContentHash != hash {
		t.Fatal("correction-only query did not find exact source", sources, err)
	}
	if sources, err := reader.Sources(ctx, correctionScope(scope), []string{"source"}, "How many reviewers are required?"); err != nil || len(sources) != 1 {
		t.Fatal("reviewer matching did not deduplicate chunk", sources, err)
	}
	if sources, err := reader.Sources(ctx, correctionScope(scope), []string{"target"}, "reviewer deadline"); err != nil || len(sources) != 0 {
		t.Fatal("candidate escaped explicit authorization", sources, err)
	}
	if err := db.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.Exec(`UPDATE ext_qa_feedback
SET feedback_anchor=json_set(feedback_anchor,'$.applies_project','松桥项目')
WHERE tenant_id='7' AND knowledge_base_id='kb' AND feedback_id='first'`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		question string
		want     int
	}{
		{"松桥项目报销期限", 1},
		{"南桥项目报销期限", 0},
		{"大松桥项目报销期限", 0},
		{"松桥项目二期报销期限", 0},
		{"报销期限", 0},
		{"非松桥项目报销期限", 0},
	} {
		sources, err := reader.SourcesForQuestion(ctx, correctionScope(scope), []string{"source"}, "5 个工作日", tc.question)
		if err != nil || len(sources) != tc.want {
			t.Errorf("question %q: sources=%+v err=%v", tc.question, sources, err)
		}
	}
	if err := db.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.Exec(`UPDATE ext_qa_feedback
SET feedback_anchor=json_remove(feedback_anchor,'$.applies_project')
WHERE tenant_id='7' AND knowledge_base_id='kb' AND feedback_id='first'`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	c := cmd("revision_create", "proposal")
	c.Payload = Payload{DocumentID: "target", GuidanceIDs: []string{first.ID}, Text: "Proposed content"}
	if _, err := s.Execute(ctx, c); !errors.Is(err, ErrConflict) {
		t.Fatal("foreign correction accepted as revision trigger", err)
	}
	c.Payload.DocumentID = "source"
	if _, err := s.Execute(ctx, c); err != nil {
		t.Fatal("same version correction could not trigger revision", err)
	}
	projection := t.TempDir()
	obstructed := filepath.Join(projection, "file-instead-of-directory")
	if err := os.WriteFile(obstructed, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := reader.Project(ctx, obstructed, correctionScope(scope)); err == nil {
		t.Fatal("blocked projection succeeded")
	}
	assertProjection := func(wantPending, wantFailed bool) {
		t.Helper()
		var pending, failed bool
		if err := db.View(ctx, func(tx *sql.Tx) error {
			return tx.QueryRow("SELECT revision<>rendered_revision,last_failure_at IS NOT NULL FROM ext_feedback_correction_projections WHERE tenant_id='7' AND knowledge_base_id='kb' AND document_id='source' AND version_id='version-source'").Scan(&pending, &failed)
		}); err != nil || pending != wantPending || failed != wantFailed {
			t.Fatalf("projection pending=%v failed=%v err=%v", pending, failed, err)
		}
	}
	assertProjection(true, true)
	if err := reader.Project(ctx, projection, correctionScope(scope)); err != nil {
		t.Fatal(err)
	}
	assertProjection(false, false)
	file := filepath.Join(projection, "7", "kb", "source", "version-source", "feedback.md")
	files, err := filepath.Glob(filepath.Join(projection, "7", "kb", "*", "*", "feedback.md"))
	if err != nil || len(files) != 1 || files[0] != file {
		t.Fatalf("three corrections did not share one file: %v %v", files, err)
	}
	body, err := os.ReadFile(file)
	_, detailBody, ok := strings.Cut(doc.Markdown, "\n\n")
	_, projectedBody, projected := strings.Cut(string(body), "\n\n")
	if err != nil || !ok || !projected || projectedBody != detailBody {
		t.Fatalf("managed Markdown entries differ from the authorized detail: %q %v", body, err)
	}
	for _, entry := range doc.Items {
		if !strings.Contains(projectedBody, "- 反馈编号："+entry.ID+"\n") {
			t.Fatalf("managed Markdown omitted %s: %q", entry.ID, body)
		}
	}
	before, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	if err := reader.Project(ctx, projection, correctionScope(scope)); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(file)
	if err != nil || !after.ModTime().Equal(before.ModTime()) {
		t.Fatal("unchanged projection was rewritten", err)
	}
	replacement := approve("replacement", "Corrected deadline: 7 个工作日", first.ID, first.Revision, withdrawn, disjoint)
	stale := approve("stale-replacement", "Corrected deadline: 10 个工作日", first.ID, first.Revision, withdrawn, disjoint)
	*clock = clock.Add(49 * time.Hour)
	if _, err := s.Execute(ctx, activation(overlap)); !errors.Is(err, ErrConflict) {
		t.Fatal("unresolved overlap activated", err)
	}
	c = activation(replacement)
	replacement, err = s.Execute(ctx, c)
	if err != nil {
		t.Fatal(replacement, err)
	}
	if again, err := s.Execute(ctx, c); err != nil || again != replacement {
		t.Fatal("activation replay changed result", again, err)
	}
	if _, err := s.Execute(ctx, activation(stale)); !errors.Is(err, ErrConflict) {
		t.Fatal("stale replacement activated", err)
	}
	c = cmd("feedback_withdraw", "withdraw")
	c.ID, c.ExpectedRevision = withdrawn.ID, withdrawn.Revision
	if _, err := s.Execute(ctx, c); err != nil {
		t.Fatal(err)
	}
	assertProjection(true, false)
	doc, err = reader.Document(ctx, correctionScope(scope), f, nil)
	if err != nil || len(doc.Items) != 2 || doc.Items[0].ID != replacement.ID || doc.Items[1].ID != disjoint.ID {
		t.Fatalf("withdraw/supersede not effective immediately: %+v %v", doc, err)
	}
	history := f
	history.State, history.History = "superseded", true
	doc, err = reader.Document(ctx, correctionScope(scope), history, nil)
	if err != nil || len(doc.Items) != 1 || doc.Items[0].ID != first.ID || doc.Items[0].Revision != first.Revision+1 {
		t.Fatalf("missing audited supersession: %+v %v", doc, err)
	}
	if err := reader.Project(ctx, projection, correctionScope(scope)); err != nil {
		t.Fatal(err)
	}
	body, err = os.ReadFile(file)
	if err != nil || strings.Contains(string(body), "5 个工作日") || strings.Contains(string(body), "Another fact") || !strings.Contains(string(body), "7 个工作日") || !strings.Contains(string(body), "Use the appeal form") {
		t.Fatal("replacement projection retained invalid corrections", err)
	}
	if err := db.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.Exec("INSERT INTO ext_document_annotations(tenant_id,knowledge_base_id,document_id,revision,effective_until,labels_json) VALUES('7','kb','source',1,?,'[]')", clock.Add(-time.Second).UTC().Format(time.RFC3339Nano))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := reader.Document(ctx, correctionScope(scope), f, nil); !errors.Is(err, corrections.ErrNotFound) {
		t.Fatal("expired source remained readable before the lifecycle sweep", err)
	}
	if sources, err := reader.Sources(ctx, correctionScope(scope), []string{"source"}, "reviewer deadline"); err != nil || len(sources) != 0 {
		t.Fatal("expired source remained discoverable", sources, err)
	}
	_, err = lifecycle.NewStore(db).Append(ctx, lifecycle.Event{EventID: "source-stale", TenantID: 7, KnowledgeBaseID: "kb", DocumentID: "source", FromState: lifecycle.Active, ToState: lifecycle.Stale, Reason: "fixture", OccurredAt: *clock})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reader.Document(ctx, correctionScope(scope), f, nil); !errors.Is(err, corrections.ErrNotFound) {
		t.Fatal("stale source remained readable", err)
	}
}
