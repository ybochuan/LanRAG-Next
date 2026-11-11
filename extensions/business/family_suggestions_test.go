package business

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/ybochuan/LanRAG-Next/extensions/weknora"
)

func TestFamilySuggestionReviewKeepsCandidatesAndGovernanceSeparate(t *testing.T) {
	s, db, create := familyFixture(t)
	ctx := t.Context()
	execute := func(c Command) Result {
		t.Helper()
		result, err := s.Execute(ctx, c)
		if err != nil {
			t.Fatal(c.Action, err)
		}
		return result
	}
	family := execute(create)
	add := cmd("family_member_add", "add-anchor")
	add.ID, add.ExpectedRevision, add.Payload.DocumentID = family.ID, family.Revision, "2026"
	family = execute(add)
	base := FamilySuggestion{
		BusinessFamilyID: family.ID, DocumentID: "2025", TargetDocumentID: "2026",
		SourceVersionID: "version-2025", TargetVersionID: "version-2026",
		Evidence: SuggestionEvidence{Similarity: 1, Declaration: 1, Structure: .8, Reasons: []string{"title_stem", "year_sequence"}},
	}
	member, relation := base, base
	member.Kind, relation.Kind, relation.EdgeType = "membership", "relationship", "amends"
	record := func() {
		t.Helper()
		if err := s.recordFamilySuggestions(ctx, create.Scope, []FamilySuggestion{member, relation}); err != nil {
			t.Fatal(err)
		}
	}
	record()
	query := Command{Scope: create.Scope, ID: family.ID}
	view, err := s.FamilySuggestions(ctx, query)
	if err != nil || len(view.Items) != 2 {
		t.Fatal(view, err)
	}
	var membership, relationship FamilySuggestion
	for _, item := range view.Items {
		if item.Kind == "membership" {
			membership = item
		} else {
			relationship = item
		}
	}
	counts := func(wantMembers, wantRelations int) {
		t.Helper()
		err := db.View(ctx, func(tx *sql.Tx) error {
			var members, relations int
			if err := tx.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM ext_business_document_family_members),
				(SELECT count(*) FROM ext_document_relations)`).Scan(&members, &relations); err != nil {
				return err
			}
			if members != wantMembers || relations != wantRelations {
				t.Fatalf("members=%d relations=%d, wanted %d/%d", members, relations, wantMembers, wantRelations)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	counts(1, 0)
	ignore := cmd("family_suggestion_ignore", "ignore-relation")
	ignore.ID, ignore.ExpectedRevision = relationship.ID, relationship.Revision
	execute(ignore)
	record()
	view, err = s.FamilySuggestions(ctx, query)
	if err != nil || len(view.Items) != 1 || view.Items[0].ID != membership.ID {
		t.Fatal("ignored candidate returned after a new scan", view, err)
	}
	confirm := cmd("family_suggestion_confirm", "confirm-member")
	confirm.ID, confirm.ExpectedRevision = membership.ID, membership.Revision
	foreign := confirm
	foreign.Scope.KnowledgeBaseID = "other"
	if _, err := s.Execute(ctx, foreign); !errors.Is(err, ErrNotFound) {
		t.Fatal("cross-KB confirm accepted", err)
	}
	if err := db.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.Exec(`CREATE TRIGGER reject_suggestion_audit BEFORE INSERT ON ext_business_audit
			WHEN NEW.action='family_suggestion_confirm' BEGIN SELECT RAISE(ABORT,'test'); END`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Execute(ctx, confirm); err == nil {
		t.Fatal("confirmation committed without audit")
	}
	counts(1, 0)
	if err := db.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.Exec("DROP TRIGGER reject_suggestion_audit")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	result := execute(confirm)
	if result.Status != "confirmed" || execute(confirm) != result {
		t.Fatal(result)
	}
	counts(2, 0)
}

func TestQueuedFamilyScanFindsRealSignalsAndRespectsLibraryPolicy(t *testing.T) {
	s, _, create := familyFixture(t)
	ctx := t.Context()
	family, err := s.Execute(ctx, create)
	if err != nil {
		t.Fatal(err)
	}
	add := cmd("family_member_add", "scan-anchor")
	add.ID, add.ExpectedRevision, add.Payload.DocumentID = family.ID, family.Revision, "2026"
	if _, err := s.Execute(ctx, add); err != nil {
		t.Fatal(err)
	}
	native := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/knowledge/batch" || r.Header.Get("X-API-Key") != "fixture-api-key" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		data := []map[string]any{}
		for _, id := range r.URL.Query()["ids"] {
			title := "员工手册（" + id + "年修订版）"
			data = append(data, map[string]any{"id": id, "title": title, "tenant_id": 7, "knowledge_base_id": "kb", "parse_status": "completed", "enable_status": "enabled"})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": data})
	}))
	defer native.Close()
	client, err := weknora.NewClient(native.URL, "fixture-api-key", 7)
	if err != nil {
		t.Fatal(err)
	}
	s.WithRemote(NewNativePublisher(client))
	scan := cmd("family_suggestion_scan", "request-first-scan")
	scan.ID = family.ID
	if result, err := s.Execute(ctx, scan); err != nil || result.Status != "queued" {
		t.Fatal(result, err)
	}
	query := Command{Scope: create.Scope, ID: family.ID}
	if out, err := s.FamilySuggestions(ctx, query); err != nil || len(out.Items) != 0 || out.ScanStatus != "queued" {
		t.Fatal(out, err)
	}
	if err := s.sweepFamilySuggestionScan(ctx, create.Scope); err != nil {
		t.Fatal(err)
	}
	out, err := s.FamilySuggestions(ctx, query)
	if err != nil || out.ScanStatus != "completed" || len(out.Items) == 0 {
		t.Fatal(out, err)
	}
	found := false
	for _, row := range out.Items {
		if row.DocumentID == "2024" || row.DocumentID == "foreign" {
			t.Fatal("archived or foreign candidate", row)
		}
		if row.Kind == "membership" && row.DocumentID == "2025" && row.TargetDocumentID == "2026" {
			found = row.Evidence.Similarity == 1 && row.Evidence.Declaration == 1
		}
	}
	if !found {
		t.Fatal("missing explainable membership", out)
	}
	policy := cmd("family_suggestion_policy_set", "new-threshold")
	policy.Payload.SuggestionPolicy = &SuggestionPolicy{SimilarityWeight: .6, DeclarationWeight: .25, StructureWeight: .15, Threshold: .95}
	if _, err := s.Execute(ctx, policy); err != nil {
		t.Fatal(err)
	}
	scan.OperationID = "request-second-scan"
	if _, err := s.Execute(ctx, scan); err != nil {
		t.Fatal(err)
	}
	if err := s.sweepFamilySuggestionScan(ctx, create.Scope); err != nil {
		t.Fatal(err)
	}
	out, err = s.FamilySuggestions(ctx, query)
	if err != nil || len(out.Items) != 0 || out.Policy.Revision != 1 {
		t.Fatal(out, err)
	}
}

func TestSuggestedReplacementStillRequiresApproval(t *testing.T) {
	s, db, _, replace := familyReplacementFixture(t)
	ctx := t.Context()
	item := FamilySuggestion{BusinessFamilyID: replace.Payload.BusinessFamilyID, Kind: "relationship", EdgeType: "supersedes",
		DocumentID: "target", TargetDocumentID: "source", SourceVersionID: "version-target", TargetVersionID: "version-source",
		Evidence: SuggestionEvidence{Similarity: 1, Declaration: 1, Structure: 1, Reasons: []string{"year_sequence"}}}
	if err := s.recordFamilySuggestions(ctx, replace.Scope, []FamilySuggestion{item}); err != nil {
		t.Fatal(err)
	}
	query := Command{Scope: replace.Scope, ID: item.BusinessFamilyID}
	view, err := s.FamilySuggestions(ctx, query)
	if err != nil || len(view.Items) != 1 {
		t.Fatal(view, err)
	}
	confirm := cmd("family_suggestion_confirm", "confirm-replacement")
	confirm.ID, confirm.ExpectedRevision = view.Items[0].ID, view.Items[0].Revision
	confirm.Payload.EffectiveAt = replace.Payload.EffectiveAt
	if _, err := s.Execute(ctx, confirm); err != nil {
		t.Fatal(err)
	}
	if err := db.View(ctx, func(tx *sql.Tx) error {
		var state string
		var holds, edges int
		err := tx.QueryRowContext(context.Background(), `SELECT status,
			(SELECT count(*) FROM ext_family_replacements WHERE status='scheduled'),
			(SELECT count(*) FROM ext_lineage_edge WHERE edge_type='supersedes') FROM ext_family_replacements`).Scan(&state, &holds, &edges)
		if err != nil || state != "pending_review" || holds != 0 || edges != 0 {
			t.Fatal(state, holds, edges, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestSuggestedRelationWritesConfirmedEdgeAndAudit(t *testing.T) {
	s, db, _, replace := familyReplacementFixture(t)
	ctx := t.Context()
	item := FamilySuggestion{BusinessFamilyID: replace.Payload.BusinessFamilyID, Kind: "relationship", EdgeType: "interprets",
		DocumentID: "target", TargetDocumentID: "source", SourceVersionID: "version-target", TargetVersionID: "version-source",
		Evidence: SuggestionEvidence{Similarity: 1, Declaration: 1, Structure: 1, Reasons: []string{"explicit_title_mention"}}}
	if err := s.recordFamilySuggestions(ctx, replace.Scope, []FamilySuggestion{item}); err != nil {
		t.Fatal(err)
	}
	confirm := cmd("family_suggestion_confirm", "confirm-interpretation")
	confirm.ID, confirm.ExpectedRevision = item.stableID(), 1
	if result, err := s.Execute(ctx, confirm); err != nil || result.Status != "confirmed" {
		t.Fatal(result, err)
	}
	if err := db.View(ctx, func(tx *sql.Tx) error {
		var edge string
		return tx.QueryRowContext(ctx, `SELECT e.edge_id FROM ext_family_suggestions s
			JOIN ext_document_relations r ON r.tenant_id=s.tenant_id AND r.knowledge_base_id=s.knowledge_base_id AND r.relation_id=s.applied_id
			JOIN ext_lineage_edge e ON e.edge_id=r.edge_id AND e.from_node_id=s.source_version_id AND e.to_node_id=s.target_version_id
			JOIN ext_business_audit a ON a.tenant_id=s.tenant_id AND a.knowledge_base_id=s.knowledge_base_id AND a.aggregate_id=s.suggestion_id
			WHERE s.status='confirmed' AND r.status='active' AND e.edge_type='interprets' AND a.action='family_suggestion_confirm'`).Scan(&edge)
	}); err != nil {
		t.Fatal(err)
	}
}

func TestFamilyScanUsesLocalSemanticEvidenceAfterSourceAuthorization(t *testing.T) {
	s, _, create := familyFixture(t)
	ctx := t.Context()
	family, err := s.Execute(ctx, create)
	if err != nil {
		t.Fatal(err)
	}
	add := cmd("family_member_add", "semantic-anchor")
	add.ID, add.ExpectedRevision, add.Payload.DocumentID = family.ID, family.Revision, "2026"
	if _, err := s.Execute(ctx, add); err != nil {
		t.Fatal(err)
	}
	const anchorTitle, peerTitle = "员工休假制度", "人事假期规则"
	var modelCalls atomic.Int32
	native := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/knowledge/batch":
			var data []map[string]any
			for _, id := range r.URL.Query()["ids"] {
				title := map[string]string{"2026": anchorTitle, "2025": peerTitle}[id]
				if title == "" {
					t.Errorf("native metadata request reached out-of-scope or archived document %s", id)
				}
				data = append(data, map[string]any{"id": id, "title": title, "tenant_id": 7, "knowledge_base_id": "kb", "parse_status": "completed", "enable_status": "enabled"})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": data})
		case "/v1/embeddings":
			var request struct {
				Model string   `json:"model"`
				Input []string `json:"input"`
			}
			if r.Header.Get("X-API-Key") != "" || r.Header.Get("Authorization") != "" ||
				json.NewDecoder(r.Body).Decode(&request) != nil || request.Model != "fixture-embedding" {
				t.Error("embedding request forwarded credentials or had the wrong model")
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			data := []map[string]any{}
			for i, title := range request.Input {
				if title != anchorTitle && title != peerTitle {
					t.Error("model received a document outside the approved candidate inputs")
				}
				data = append(data, map[string]any{"index": i, "embedding": []float64{1, 0}})
			}
			modelCalls.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{"model": request.Model, "data": data})
		default:
			t.Error("unexpected endpoint", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer native.Close()
	client, err := weknora.NewClient(native.URL, "fixture-native-key", 7)
	if err != nil {
		t.Fatal(err)
	}
	s.WithRemote(NewNativePublisher(client))
	model, err := NewLocalFamilyEmbedder(native.URL, "fixture-embedding")
	if err != nil {
		t.Fatal(err)
	}
	s.WithFamilyEmbedder(model)
	policy := cmd("family_suggestion_policy_set", "semantic-threshold")
	policy.Payload.SuggestionPolicy = &SuggestionPolicy{SimilarityWeight: .6, DeclarationWeight: .25, StructureWeight: .15, Threshold: .55}
	if _, err := s.Execute(ctx, policy); err != nil {
		t.Fatal(err)
	}
	scan := cmd("family_suggestion_scan", "semantic-scan")
	scan.ID = family.ID
	if _, err := s.Execute(ctx, scan); err != nil {
		t.Fatal(err)
	}
	if err := s.sweepFamilySuggestionScan(ctx, create.Scope); err != nil {
		t.Fatal(err)
	}
	inbox, err := s.FamilySuggestions(ctx, Command{Scope: create.Scope, ID: family.ID})
	if err != nil || len(inbox.Items) != 1 || modelCalls.Load() == 0 {
		t.Fatal(inbox, err, modelCalls.Load())
	}
	row := inbox.Items[0]
	if row.EdgeType != "related_to" || row.Evidence.SemanticSimilarity == nil || *row.Evidence.SemanticSimilarity != 1 ||
		row.Evidence.TitleSimilarity != 0 || row.Evidence.SemanticModel != "fixture-embedding" {
		t.Fatal(row)
	}
	confirm := cmd("family_suggestion_confirm", "confirm-semantic-relation")
	confirm.ID, confirm.ExpectedRevision = row.ID, row.Revision
	if result, err := s.Execute(ctx, confirm); err != nil || result.Status != "confirmed" {
		t.Fatal(result, err)
	}
	related, err := s.QueryRelated(ctx, create.Scope, "2026")
	if err != nil || len(related) != 1 || related[0].DocumentID != "2025" || related[0].Confidence != .6 {
		t.Fatal("confirmed relation should honor the calibrated threshold", related, err)
	}
	if _, err := NewLocalFamilyEmbedder("http://192.0.2.1:8081", "fixture-embedding"); err == nil {
		t.Fatal("accepted a nonlocal model origin")
	}
}
