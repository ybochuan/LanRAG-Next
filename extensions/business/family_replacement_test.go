package business

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/contract"
	"github.com/ybochuan/LanRAG-Next/extensions/lifecycle"
	"github.com/ybochuan/LanRAG-Next/extensions/lineage"
	"github.com/ybochuan/LanRAG-Next/extensions/provenance"
	"github.com/ybochuan/LanRAG-Next/extensions/retrieval"
	"github.com/ybochuan/LanRAG-Next/extensions/store"
	"github.com/ybochuan/LanRAG-Next/extensions/weknora"
)

func familyReplacementFixture(t *testing.T) (*Service, *store.DB, *time.Time, Command) {
	t.Helper()
	s, db, clock, _ := repealFixture(t)
	create := cmd("family_create", "create-family")
	create.Payload.FamilyLabel = "Handbook"
	f, err := s.Execute(t.Context(), create)
	if err != nil {
		t.Fatal(err)
	}
	for _, doc := range []string{"source", "target"} {
		add := cmd("family_member_add", "member-"+doc)
		add.ID, add.ExpectedRevision, add.Payload.DocumentID = f.ID, f.Revision, doc
		f, err = s.Execute(t.Context(), add)
		if err != nil {
			t.Fatal(err)
		}
	}
	due := clock.Add(time.Hour)
	replace := cmd("family_replace_create", "create-replacement")
	replace.Payload = Payload{BusinessFamilyID: f.ID, FamilyRevision: f.Revision,
		DocumentID: "target", TargetDocumentID: "source", EffectiveAt: &due}
	return s, db, clock, replace
}

func TestFamilyReplacementGateAndAtomicCutover(t *testing.T) {
	ctx := t.Context()
	s, db, clock, create := familyReplacementFixture(t)
	p, err := retrieval.NewPolicy(db, create.Scope.TenantID, []string{create.Scope.KnowledgeBaseID})
	if err != nil {
		t.Fatal(err)
	}
	p.WithNow(func() time.Time { return *clock })
	permits := func(excluded ...string) {
		t.Helper()
		for _, mode := range []string{"rag", "agent"} {
			r := contract.BeforeRetrieveRequest{Context: contract.Context{RequestID: "fixture", SessionID: "fixture", Mode: mode,
				TenantID: create.Scope.TenantID, KnowledgeBaseIDs: []string{create.Scope.KnowledgeBaseID}, KnowledgeIDs: []string{"source", "target"}}}
			got, err := p.ExcludedDocumentIDs(ctx, r)
			if err != nil || !slices.Equal(got, excluded) {
				t.Fatalf("%s excluded %v, %v; expected %v", mode, got, err, excluded)
			}
		}
	}
	draft, err := s.Execute(ctx, create)
	if err != nil || draft.Status != "pending_review" {
		t.Fatal(draft, err)
	}
	remove := cmd("family_member_remove", "remove-locked-member")
	remove.ID, remove.ExpectedRevision, remove.Payload.DocumentID = create.Payload.BusinessFamilyID, create.Payload.FamilyRevision, "target"
	if _, err := s.Execute(ctx, remove); !errors.Is(err, ErrConflict) {
		t.Fatal("member referenced by a pending replacement was removed", err)
	}
	permits()
	approve := cmd("family_replace_approve", "approve-replacement")
	approve.ID, approve.ExpectedRevision = draft.ID, draft.Revision
	if _, err := s.Execute(ctx, approve); !errors.Is(err, ErrInvalid) {
		t.Fatal("approval lacked explicit confirmation", err)
	}
	approve.Payload.SecondConfirmation = true
	scheduled, err := s.Execute(ctx, approve)
	if err != nil || scheduled.Status != "scheduled" {
		t.Fatal(scheduled, err)
	}
	permits("target")
	if _, err := s.Execute(ctx, remove); !errors.Is(err, ErrConflict) {
		t.Fatal("scheduled member was removed", err)
	}
	scopes := []provenance.Scope{create.Scope}
	if err := s.SweepFamilyReplacements(ctx, scopes); err != nil {
		t.Fatal(err)
	}
	permits("target")
	*clock = *create.Payload.EffectiveAt
	if err := db.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.Exec(`CREATE TRIGGER fail_family_cutover BEFORE INSERT ON ext_business_audit
			WHEN NEW.action='family_replace_apply' BEGIN SELECT RAISE(ABORT,'fixture'); END`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.SweepFamilyReplacements(ctx, scopes); err == nil {
		t.Fatal("cutover committed without an audit")
	}
	permits("source", "target")
	if err := db.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.Exec(`DROP TRIGGER fail_family_cutover`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := s.SweepFamilyReplacements(ctx, scopes); err != nil {
			t.Fatal(err)
		}
	}
	permits("source")
	families, err := s.Families(ctx, Command{Scope: create.Scope, ID: create.Payload.BusinessFamilyID})
	if err != nil || len(families) != 1 || families[0].PublishedVersionNodeID != "version-target" ||
		families[0].PublishedDocumentID != "target" || families[0].Revision != create.Payload.FamilyRevision+1 {
		t.Fatal(families, err)
	}
	if err := db.View(ctx, func(tx *sql.Tx) error {
		var count int
		err := tx.QueryRow(`SELECT count(*) FROM ext_lineage_edge
			WHERE edge_type='supersedes' AND from_node_id='version-target' AND to_node_id='version-source'`).Scan(&count)
		if err != nil || count != 1 {
			t.Fatalf("critical edge missing or duplicated: %d %v", count, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestFamilyReplacementTimelineShowsOldestFirst(t *testing.T) {
	s, _, _, create := familyReplacementFixture(t)
	ctx := t.Context()
	first, err := s.Execute(ctx, create)
	if err != nil {
		t.Fatal(err)
	}
	later := create.Payload.EffectiveAt.Add(time.Hour)
	create.OperationID = "later-timeline-plan"
	create.Payload.EffectiveAt = &later
	second, err := s.Execute(ctx, create)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := s.FamilyReplacements(ctx, Command{Scope: create.Scope, ID: create.Payload.BusinessFamilyID, ListStatus: "timeline"})
	if err != nil || len(rows) != 2 || rows[0].ID != first.ID || rows[1].ID != second.ID {
		t.Fatal("timeline did not follow the business effective dates", rows, err)
	}
}

func TestFamilyReplacementHistoryCommitsFiveVersionChainAtomically(t *testing.T) {
	ctx := t.Context()
	s, db, clock, create := familyReplacementFixture(t)
	familyID := create.Payload.BusinessFamilyID
	familyRevision := create.Payload.FamilyRevision
	docs := []string{"source", "target", "v2020", "v2024", "v2024sep"}
	for _, doc := range docs[2:] {
		seedSplitTarget(t, db, doc)
		add := cmd("family_member_add", "history-member-"+doc)
		add.ID, add.ExpectedRevision, add.Payload.DocumentID = familyID, familyRevision, doc
		result, err := s.Execute(ctx, add)
		if err != nil {
			t.Fatal(err)
		}
		familyRevision = result.Revision
	}
	var drafts []Result
	for i := 1; i < len(docs); i++ {
		due := clock.Add(time.Duration(i) * time.Hour)
		draft := create
		draft.OperationID = "history-plan-" + docs[i]
		draft.Payload.FamilyRevision = familyRevision
		draft.Payload.TargetDocumentID, draft.Payload.DocumentID, draft.Payload.EffectiveAt = docs[i-1], docs[i], &due
		result, err := s.Execute(ctx, draft)
		if err != nil {
			t.Fatal(err)
		}
		drafts = append(drafts, result)
	}
	first := cmd("family_replace_approve", "history-approve-first")
	first.ID, first.ExpectedRevision, first.Payload.SecondConfirmation = drafts[0].ID, drafts[0].Revision, true
	if _, err := s.Execute(ctx, first); err != nil {
		t.Fatal(err)
	}
	// The fixture creates a future chain so live creation can enforce a future
	// effective date. Move the persisted records into the past to exercise the
	// historical recovery path, whose real records are already overdue.
	if err := db.Write(ctx, func(tx *sql.Tx) error {
		for i, draft := range drafts {
			due := clock.Add(time.Duration(i-3) * time.Hour)
			if _, err := tx.Exec(`UPDATE ext_family_replacements SET effective_at_ms=? WHERE replacement_id=?`,
				due.UnixMilli(), draft.ID); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.SweepFamilyReplacements(ctx, []provenance.Scope{create.Scope}); err != nil {
		t.Fatal(err)
	}
	query := Command{Scope: create.Scope, ID: familyID}
	preview, err := s.FamilyReplacementHistory(ctx, query)
	if err != nil || preview.Fingerprint == "" || preview.PublishedDocumentID != "target" ||
		preview.FinalDocumentID != "v2024sep" || len(preview.Steps) != 3 || preview.FamilyRevision != familyRevision+1 {
		t.Fatal("historical suffix cannot be previewed", preview, err)
	}
	apply := cmd("family_replace_history", "history-batch")
	apply.ID, apply.ExpectedRevision = familyID, preview.FamilyRevision
	apply.Payload = Payload{SecondConfirmation: true, HistoryFingerprint: preview.Fingerprint,
		ReplacementIDs: []string{drafts[1].ID, drafts[2].ID, drafts[3].ID}}
	stale := apply
	stale.Payload.HistoryFingerprint = strings.Repeat("0", 64)
	if _, err := s.Execute(ctx, stale); !errors.Is(err, ErrConflict) {
		t.Fatal("stale preview accepted", err)
	}
	forkDate := clock.Add(time.Hour)
	fork := create
	fork.OperationID = "history-fork"
	fork.Payload.FamilyRevision = preview.FamilyRevision
	fork.Payload.TargetDocumentID, fork.Payload.DocumentID, fork.Payload.EffectiveAt = "target", "v2024sep", &forkDate
	branch, err := s.Execute(ctx, fork)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.Exec(`UPDATE ext_family_replacements SET effective_at_ms=? WHERE replacement_id=?`,
			clock.Add(-30*time.Minute).UnixMilli(), branch.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	blocked, err := s.FamilyReplacementHistory(ctx, query)
	if err != nil || blocked.BlockedReason != "disconnected_history" || blocked.Fingerprint != "" {
		t.Fatal("parallel drafts did not block the history preview", blocked, err)
	}
	if _, err := s.Execute(ctx, apply); !errors.Is(err, ErrConflict) {
		t.Fatal("previous preview accepted after a new fork", err)
	}
	cancel := cmd("family_replace_cancel", "cancel-history-fork")
	cancel.ID, cancel.ExpectedRevision = branch.ID, branch.Revision
	if _, err := s.Execute(ctx, cancel); err != nil {
		t.Fatal(err)
	}
	check := func(head string, count int, excluded []string) {
		t.Helper()
		families, err := s.Families(ctx, query)
		if err != nil || len(families) != 1 || families[0].PublishedDocumentID != head {
			t.Fatal("unexpected publication head", families, err)
		}
		policy, err := retrieval.NewPolicy(db, create.Scope.TenantID, []string{create.Scope.KnowledgeBaseID})
		if err != nil {
			t.Fatal(err)
		}
		policy.WithNow(func() time.Time { return *clock })
		got, err := policy.ExcludedDocumentIDs(ctx, contract.BeforeRetrieveRequest{Context: contract.Context{
			RequestID: "history-fixture", SessionID: "history-fixture", Mode: "rag", TenantID: create.Scope.TenantID,
			KnowledgeBaseIDs: []string{create.Scope.KnowledgeBaseID}, KnowledgeIDs: docs,
		}})
		if err != nil || !slices.Equal(got, excluded) {
			t.Fatal("wrong retrieval visibility", got, err)
		}
		if err := db.View(ctx, func(tx *sql.Tx) error {
			var edges, applied int
			err := tx.QueryRow(`SELECT (SELECT count(*) FROM ext_lineage_edge WHERE edge_type='supersedes'),
				(SELECT count(*) FROM ext_family_replacements WHERE status='applied')`).Scan(&edges, &applied)
			if edges != count || applied != count {
				t.Fatalf("edges=%d applied=%d expected=%d", edges, applied, count)
			}
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	check("target", 1, docs[:1])
	if err := db.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.Exec(`CREATE TRIGGER history_fail_last BEFORE INSERT ON ext_lineage_edge
			WHEN NEW.from_node_id='version-v2024sep' BEGIN SELECT RAISE(ABORT,'fixture'); END`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Execute(ctx, apply); err == nil {
		t.Fatal("batch committed despite a late failure")
	}
	check("target", 1, docs[:1])
	if err := db.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.Exec(`DROP TRIGGER history_fail_last`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	result, err := s.Execute(ctx, apply)
	if err != nil || result.Status != "applied" || result.TargetDocumentID != "v2024sep" {
		t.Fatal("historical cutover failed", result, err)
	}
	if replay, err := s.Execute(ctx, apply); err != nil || replay != result {
		t.Fatal("history operation did not replay", replay, err)
	}
	check("v2024sep", 4, docs[:4])
	activity, err := s.DocumentActivity(ctx, Command{Scope: create.Scope, Payload: Payload{DocumentID: "v2020"}})
	if err != nil || !slices.ContainsFunc(activity, func(row WorkspaceActivity) bool { return row.OperationID == apply.OperationID }) {
		t.Fatal("intermediate document cannot find its history audit", activity, err)
	}
}

func TestFamilyReplacementCompetingApprovalAndDrift(t *testing.T) {
	ctx := t.Context()
	s, db, clock, create := familyReplacementFixture(t)
	draft, err := s.Execute(ctx, create)
	if err != nil {
		t.Fatal(err)
	}
	create.OperationID = "competing-replacement"
	other, err := s.Execute(ctx, create)
	if err != nil {
		t.Fatal(err)
	}
	outcomes := make(chan error, 2)
	for _, candidate := range []Result{draft, other} {
		go func() {
			c := cmd("family_replace_approve", "approve-"+candidate.ID)
			c.ID, c.ExpectedRevision, c.Payload.SecondConfirmation = candidate.ID, candidate.Revision, true
			_, err := s.Execute(ctx, c)
			outcomes <- err
		}()
	}
	first, second := <-outcomes, <-outcomes
	if !((first == nil && errors.Is(second, ErrConflict)) || (second == nil && errors.Is(first, ErrConflict))) {
		t.Fatal("expected one scheduled replacement", first, second)
	}
	verify := s.verify
	s.verify = func(ctx context.Context, scope provenance.Scope, doc string) (string, error) {
		if doc == "target" {
			return "drifted-version", nil
		}
		return verify(ctx, scope, doc)
	}
	*clock = *create.Payload.EffectiveAt
	if err := s.SweepFamilyReplacements(ctx, []provenance.Scope{create.Scope}); err != nil {
		t.Fatal(err)
	}
	if err := db.View(ctx, func(tx *sql.Tx) error {
		state, _, err := store.ReadLifecycleCurrentTx(ctx, tx, 7, "kb", "source")
		if err != nil || state != "active" {
			t.Fatalf("drift retired the old member: %s %v", state, err)
		}
		var count int
		if err := tx.QueryRow(`SELECT count(*) FROM ext_family_replacements WHERE status='needs_review'`).Scan(&count); err != nil || count != 1 {
			t.Fatalf("drift was not sent for review: %d %v", count, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestFamilyReplacementRejectsAttachmentEndpoints(t *testing.T) {
	s, db, _, create := familyReplacementFixture(t)
	ctx := t.Context()
	parentHash, childHash := strings.Repeat("a", 32), strings.Repeat("b", 32)
	id, err := db.PrepareAttachmentImport(ctx, create.Scope.TenantID, create.Scope.KnowledgeBaseID,
		"source", parentHash, strings.Repeat("c", 64), childHash)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.ClaimAttachmentUpload(ctx, id); err != nil {
		t.Fatal(err)
	}
	if err := db.BindAttachments(ctx, create.Scope.TenantID, create.Scope.KnowledgeBaseID, "source", parentHash,
		[]store.AttachmentBinding{{ImportID: id, AttachmentID: "target"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Execute(ctx, create); !errors.Is(err, ErrConflict) {
		t.Fatal("old document's attachment could replace its parent", err)
	}
}

func TestFamilyReplacementChecksNativeEvidenceAndKeepsAttachmentHistory(t *testing.T) {
	s, db, clock, create := familyReplacementFixture(t)
	ctx := t.Context()
	parentHash, childHash := strings.Repeat("a", 32), strings.Repeat("b", 32)
	importID, err := db.PrepareAttachmentImport(ctx, 7, "kb", "source", parentHash, strings.Repeat("c", 64), childHash)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.ClaimAttachmentUpload(ctx, importID); err != nil {
		t.Fatal(err)
	}
	if err := db.BindAttachments(ctx, 7, "kb", "source", parentHash,
		[]store.AttachmentBinding{{ImportID: importID, AttachmentID: "child"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := lifecycle.NewStore(db).Append(ctx, lifecycle.Event{
		EventID: "child-active", TenantID: 7, KnowledgeBaseID: "kb", DocumentID: "child",
		ToState: lifecycle.Active, Reason: "fixture", OccurredAt: *clock,
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Write(ctx, func(tx *sql.Tx) error {
		for doc, hash := range map[string]string{"source": parentHash, "target": ""} {
			body, err := json.Marshal(map[string]any{"header_fields": map[string]string{
				"file_hash": digest(hash), "enable_status": digest("enabled"), "parse_status": digest("completed"), "channel": digest(""),
			}})
			if err != nil {
				return err
			}
			if _, err := tx.Exec(`UPDATE ext_source_snapshot_evidence SET snapshot_json=? WHERE version_node_id=?`, string(body), "version-"+doc); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var changed atomic.Bool
	native := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		list := r.URL.Path == "/api/v1/knowledge-bases/kb/knowledge"
		if !list && r.URL.Path != "/api/v1/knowledge/batch" || r.Header.Get("X-API-Key") != "test-api-key" {
			http.Error(w, "unexpected native request", http.StatusBadRequest)
			return
		}
		data := []map[string]any{}
		ids := r.URL.Query()["ids"]
		if list {
			ids = []string{"source", "target", "child"}
		}
		for _, id := range ids {
			doc := map[string]any{"id": id, "tenant_id": 7, "knowledge_base_id": "kb", "parse_status": "completed", "enable_status": "enabled"}
			switch id {
			case "source":
				doc["file_hash"] = parentHash
				if changed.Load() {
					doc["channel"] = "changed-after-snapshot"
				}
			case "child":
				doc["file_hash"], doc["file_name"] = childHash, "form.pdf"
				doc["metadata"] = map[string]string{"is_attachment": "true", "parent_document_id": "source", "attachment_import_id": importID}
			}
			data = append(data, doc)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": data, "total": len(data), "page": 1, "page_size": 100})
	}))
	defer native.Close()
	client, err := weknora.NewClient(native.URL, "test-api-key", 7)
	if err != nil {
		t.Fatal(err)
	}
	s.WithRemote(NewNativePublisher(client))
	add := cmd("family_member_add", "add-attachment")
	add.ID, add.ExpectedRevision, add.Payload.DocumentID = create.Payload.BusinessFamilyID, create.Payload.FamilyRevision, "child"
	if _, err := s.Execute(ctx, add); !errors.Is(err, ErrConflict) {
		t.Fatal("native attachment joined the business family", err)
	}
	draft, err := s.Execute(ctx, create)
	if err != nil {
		t.Fatal("a primary document with a bound attachment must be eligible", err)
	}
	approve := cmd("family_replace_approve", "approve-native-replacement")
	approve.ID, approve.ExpectedRevision, approve.Payload.SecondConfirmation = draft.ID, draft.Revision, true
	changed.Store(true)
	if _, err := s.Execute(ctx, approve); !errors.Is(err, ErrConflict) {
		t.Fatal("approval used metadata that disagreed with the observed version", err)
	}
	changed.Store(false)
	if _, err := s.Execute(ctx, approve); err != nil {
		t.Fatal(err)
	}
	policy, err := retrieval.NewPolicy(db, 7, []string{"kb"})
	if err != nil {
		t.Fatal(err)
	}
	policy.WithAttachments(client)
	policy.WithNow(func() time.Time { return *clock })
	request := contract.BeforeRetrieveRequest{Context: contract.Context{RequestID: "fixture", SessionID: "fixture", Mode: "rag",
		TenantID: 7, KnowledgeBaseIDs: []string{"kb"}, KnowledgeIDs: []string{"child"}}}
	check := func(allowed bool) {
		t.Helper()
		excluded, err := policy.ExcludedDocumentIDs(ctx, request)
		if err != nil || (len(excluded) == 0) != allowed {
			t.Fatalf("attachment allowed=%t, excluded=%v, err=%v", allowed, excluded, err)
		}
		links, err := policy.AttachmentLinks(ctx, request)
		if err != nil || (len(links["child"]) == 1) != allowed {
			t.Fatalf("attachment allowed=%t, links=%v, err=%v", allowed, links, err)
		}
	}
	check(true)
	*clock = *create.Payload.EffectiveAt
	if err := s.SweepFamilyReplacements(ctx, []provenance.Scope{create.Scope}); err != nil {
		t.Fatal(err)
	}
	check(false)
	if err := db.View(ctx, func(tx *sql.Tx) error {
		owners, err := store.AttachmentOwnershipsTx(ctx, tx, 7, []string{"child"})
		if owners["child"].ParentDocumentID != "source" {
			t.Fatal("attachment's old ownership must remain in history", owners)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

func declareReplacementFixtureDependency(t *testing.T, db *store.DB, child, parent string) {
	t.Helper()
	err := db.Write(t.Context(), func(tx *sql.Tx) error {
		const at = "2026-11-02T00:00:00Z"
		if _, err := tx.Exec(`INSERT INTO ext_supplement_declarations VALUES(
			'fixture','7','kb',?,1,?,?,'actor','op',?,'active',?)`,
			child, "version-"+child, child+"-active", strings.Repeat("f", 64), at); err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO ext_supplement_parents VALUES('fixture','7','kb',?,?,?,?)`,
			child, parent, "version-"+parent, parent+"-active"); err != nil {
			return err
		}
		_, err := tx.Exec(`INSERT INTO ext_supplement_current VALUES('7','kb',?,1,'fixture')`, child)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestFamilyReplacementRejectsHoldingDependencyOfOldMember(t *testing.T) {
	s, db, _, create := familyReplacementFixture(t)
	declareReplacementFixtureDependency(t, db, "source", "target")
	if _, err := s.Execute(t.Context(), create); !errors.Is(err, ErrConflict) {
		t.Fatal("reservation could make the old member unreadable", err)
	}
}

func TestFamilyReplacementChecksFreshThirdPartyDependency(t *testing.T) {
	t.Run("drifted", func(t *testing.T) { checkFamilyDependencyChanged(t, "fresh-third-version", nil) })
	t.Run("missing", func(t *testing.T) { checkFamilyDependencyChanged(t, "", lineage.ErrSourceMissing) })
}

func checkFamilyDependencyChanged(t *testing.T, refreshed string, refreshErr error) {
	s, db, clock, create := familyReplacementFixture(t)
	ctx := t.Context()
	if _, err := lifecycle.NewStore(db).Append(ctx, lifecycle.Event{EventID: "third-active", TenantID: 7,
		KnowledgeBaseID: "kb", DocumentID: "third", ToState: lifecycle.Active, Reason: "fixture", OccurredAt: *clock}); err != nil {
		t.Fatal(err)
	}
	if err := db.Write(ctx, func(tx *sql.Tx) error {
		stamp := clock.Format(publicationTime)
		if _, err := tx.Exec(`INSERT INTO ext_lineage_node VALUES('version-third','7','kb','document_version','third',?)`, stamp); err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO ext_document_version_family VALUES('family-third','7','kb','third','','version-third','version-third',1,?,?)`, stamp, stamp); err != nil {
			return err
		}
		_, err := tx.Exec(`INSERT INTO ext_document_version VALUES('version-third','7','kb','family-third','third',?,1,'third-active','','fixture',?)`, textHash("third"), stamp)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	declareReplacementFixtureDependency(t, db, "target", "third")
	draft, err := s.Execute(ctx, create)
	if err != nil {
		t.Fatal(err)
	}
	approve := cmd("family_replace_approve", "approve-dependency")
	approve.ID, approve.ExpectedRevision, approve.Payload.SecondConfirmation = draft.ID, draft.Revision, true
	if _, err := s.Execute(ctx, approve); err != nil {
		t.Fatal(err)
	}
	oldVerify := s.verify
	s.verify = func(ctx context.Context, scope provenance.Scope, doc string) (string, error) {
		if doc == "third" {
			return refreshed, refreshErr
		}
		return oldVerify(ctx, scope, doc)
	}
	*clock = *create.Payload.EffectiveAt
	if err := s.SweepFamilyReplacements(ctx, []provenance.Scope{create.Scope}); err != nil {
		t.Fatal(err)
	}
	plans, err := s.FamilyReplacements(ctx, Command{Scope: create.Scope, ID: create.Payload.BusinessFamilyID})
	if err != nil || len(plans) != 1 || plans[0].Status != "needs_review" {
		t.Fatal("stale dependency was trusted", plans, err)
	}
	if err := db.View(ctx, func(tx *sql.Tx) error {
		state, _, err := store.ReadLifecycleCurrentTx(ctx, tx, 7, "kb", "source")
		if err == nil && state != "active" {
			t.Fatal("dependency drift retired the old document")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}
