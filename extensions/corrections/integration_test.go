package corrections_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/bridge"
	"github.com/ybochuan/LanRAG-Next/extensions/corrections"
	"github.com/ybochuan/LanRAG-Next/extensions/lifecycle"
	"github.com/ybochuan/LanRAG-Next/extensions/management"
	"github.com/ybochuan/LanRAG-Next/extensions/retention"
	"github.com/ybochuan/LanRAG-Next/extensions/store"
)

func TestCorrectionUpgradeBridgeManagementAndRetention(t *testing.T) {
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "ext.db")
	db, err := store.OpenDurable(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	hash := strings.Repeat("a", 64)
	// Build a schema-40 fixture with the production lineage, feedback and run tables.
	if _, err := lifecycle.NewStore(db).Append(ctx, lifecycle.Event{EventID: "event", TenantID: 7, KnowledgeBaseID: "kb", DocumentID: "doc", ToState: lifecycle.Active, Reason: "fixture", OccurredAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := db.Write(ctx, func(tx *sql.Tx) error {
		for _, stmt := range []string{
			"DROP TRIGGER ext_feedback_correction_project_status",
			"DROP TABLE ext_feedback_correction_projections",
			"DROP TABLE ext_feedback_correction_replacements",
			"DROP TABLE ext_feedback_corrections",
			"DELETE FROM ext_schema_migrations WHERE version=41",
			"INSERT INTO ext_lineage_node VALUES('v1','7','kb','document_version','doc','2026-01-01T00:00:00Z')",
			"INSERT INTO ext_document_version_family VALUES('fam','7','kb','doc','','v1','v1',1,'2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')",
			"INSERT INTO ext_document_version VALUES('v1','7','kb','fam','doc','" + hash + "',1,'event','','fixture','2026-01-01T00:00:00Z')",
			`INSERT INTO ext_source_snapshot_evidence VALUES('7','kb','v1','{"chunks":[{"id":"chunk","content_hash":"` + hash + `","chunk_index":2}]}','` + hash + `')`,
			"INSERT INTO ext_provenance_runs VALUES('7','kb','old-run',2,'sealed','" + hash + "','{}')",
			"INSERT INTO ext_provenance_run_receipts VALUES('7','kb','old-op','old-run','begin','" + hash + "','{}')",
			"INSERT INTO ext_provenance_run_audit(sequence,tenant_id,knowledge_base_id,operation_id,run_id,revision,state,occurred_at) VALUES(1,'7','kb','old-op','old-run',2,'sealed','2026-01-01T00:00:00Z')",
			"INSERT INTO ext_provenance_steps VALUES('7','kb','old-run','step','model','success','" + hash + "','" + hash + "')",
			"INSERT INTO ext_provenance_inputs VALUES('7','kb','old-run','step','old-op','" + hash + "','[]','chunk')",
		} {
			if _, err := tx.Exec(stmt); err != nil {
				return err
			}
		}
		for _, id := range []string{"review", "invalid-pin", "cooling"} {
			anchorHash, status := hash, "active"
			if id == "invalid-pin" {
				anchorHash = strings.Repeat("b", 64)
			}
			if id == "cooling" {
				status = "approved"
			}
			if _, err := tx.Exec(`INSERT INTO ext_qa_feedback VALUES('7','kb',?,'2026-01-01T00:00:00Z','correction','','private-submission','{"answer_excerpt":"private-answer"}','[]',json_object('document_id','doc','version_id','v1','chunk_id','chunk','chunk_index',2,'content_hash',?),'Submission deadline','Deadline is 5 个工作日','approved',2)`, id, anchorHash); err != nil {
				return err
			}
			if _, err := tx.Exec(`INSERT INTO ext_feedback_events VALUES('7','kb',?,'manual','topic','',0,'2026-01-01T00:00:00Z')`, id); err != nil {
				return err
			}
			if _, err := tx.Exec(`INSERT INTO ext_feedback_staging VALUES('7','kb',?,'topic','legacy','medium',?,3,NULL)`, id, status); err != nil {
				return err
			}
			if status == "active" {
				if _, err := tx.Exec(`INSERT INTO ext_feedback_guidances VALUES('7','kb',?,?,'feedback','topic','legacy',1,'active',1,'2026-01-04T00:00:00Z')`, id, id); err != nil {
					return err
				}
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = store.OpenDurable(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	now := time.Now()
	if result, err := retention.New(db, 7, []string{"kb"}, 30*24*time.Hour).Sweep(ctx, "migration-test"); err != nil || len(result) != 1 || result[0].InputsDeleted != 1 {
		t.Fatal("old run evidence was not pruned", result, err)
	}
	service := corrections.New(db, func() time.Time { return now })
	key := strings.Repeat("k", 32)
	native := &bridge.CorrectionSourcesHandler{Key: key, TenantID: 7, KnowledgeBaseIDs: []string{"kb"}, Corrections: service}
	bridgeHandler := bridge.NewServerWithServices(bridge.Services{CorrectionSources: native}).Handler
	manager := management.New(db, key, []lifecycle.PolicyScope{{TenantID: 7, KnowledgeBaseID: "kb"}}).WithNativeSQLite("", func(_ context.Context, kb string, ids []string) (map[string]string, error) {
		if kb != "kb" || len(ids) != 1 || ids[0] != "doc" {
			t.Fatalf("unexpected title scope: %s %v", kb, ids)
		}
		return map[string]string{"doc": "Renamed title"}, nil
	})
	call := func(handler http.Handler, path, body, credential, version string, want int) string {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+credential)
		req.Header.Set(bridge.VersionHeader, version)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != want {
			t.Fatalf("%s: got %d, want %d, body %s", path, w.Code, want, w.Body)
		}
		return strings.TrimSpace(w.Body.String())
	}
	discover := `{"tenant_id":7,"knowledge_base_id":"kb","knowledge_ids":["doc","doc"],"query":"哪项规定是 5 个工作日"}`
	const bridgePath = "/bridge/correction-sources"
	got := call(bridgeHandler, bridgePath, discover, key, "1", 200)
	if want := `{"sources":[{"document_id":"doc","chunk_id":"chunk","content_hash":"` + hash + `"}]}`; got != want {
		t.Fatal("bridge disclosed extra fields or lost migrated source", got)
	}
	call(bridgeHandler, bridgePath, discover, "wrong", "1", 401)
	call(bridgeHandler, bridgePath, discover, key, "2", 400)
	call(bridgeHandler, bridgePath, strings.Replace(discover, `"kb"`, `"foreign-kb"`, 1), key, "1", 403)
	call(bridgeHandler, bridgePath, strings.Replace(discover, `"tenant_id":7`, `"tenant_id":8`, 1), key, "1", 403)
	call(bridgeHandler, bridgePath, strings.Replace(discover, `"query"`, `"unknown"`, 1), key, "1", 400)
	for _, body := range []string{strings.Replace(discover, `["doc","doc"]`, "[]", 1), strings.Replace(discover, `["doc","doc"]`, `["wrong"]`, 1), strings.Replace(discover, "哪项规定是 5 个工作日", "weather forecast", 1)} {
		if got := call(bridgeHandler, bridgePath, body, key, "1", 200); got != `{"sources":[]}` {
			t.Fatal("discovery returned an unscoped or non-matching source", got)
		}
	}
	native.EnabledKnowledgeBaseIDs = []string{}
	if got := call(bridgeHandler, bridgePath, discover, key, "1", 200); got != `{"sources":[]}` {
		t.Fatal("disabled rollout still discovers sources", got)
	}
	const managementPath = "/management/lifecycle"
	list := `{"action":"business_correction_documents","tenant_id":7,"knowledge_base_id":"kb"}`
	var listResult struct {
		Data corrections.Page `json:"data"`
	}
	if err := json.Unmarshal([]byte(call(manager, managementPath, list, key, "1", 200)), &listResult); err != nil || listResult.Data.Total != 1 || len(listResult.Data.Items) != 1 || listResult.Data.Items[0].ActiveCount != 1 || listResult.Data.Items[0].FileName != "Renamed title反馈文档.md" {
		t.Fatalf("wrong read-only listing: %+v %v", listResult, err)
	}
	detail := `{"action":"business_correction_document","tenant_id":7,"knowledge_base_id":"kb","document_id":"doc","version_id":"v1","query":"deadline","approved_from":"2026-01-01","approved_to":"2026-01-01"}`
	got = call(manager, managementPath, detail, key, "1", 200)
	if !strings.Contains(got, "5 个工作日") || strings.Contains(got, "private-") || strings.Contains(got, "invalid-pin") || strings.Contains(got, "cooling") {
		t.Fatal("wrong detail migration or restricted content", got)
	}
	download := strings.Replace(detail, "business_correction_document", "business_correction_markdown", 1)
	if got := call(manager, managementPath, download, key, "1", 200); !strings.Contains(got, "5 个工作日") || strings.Contains(got, `"items"`) {
		t.Fatal("active download returned an unexpected payload", got)
	}
	audit := `{"action":"business_correction_history","tenant_id":7,"knowledge_base_id":"kb","document_id":"doc","version_id":"v1","business":{"id":"review"}}`
	call(manager, managementPath, audit, key, "1", 403)
	audit = strings.TrimSuffix(audit, "}") + `,"can_write":true,"page_size":1}`
	if got := call(manager, managementPath, audit, key, "1", 200); !strings.Contains(got, `"current_state":"active"`) || !strings.Contains(got, `"events":[]`) {
		t.Fatal("legacy record should report current state with an empty audit history", got)
	}
	call(manager, managementPath, strings.Replace(detail, `"v1"`, `"wrong"`, 1), key, "1", 404)
	call(manager, managementPath, strings.TrimSuffix(detail, "}")+`,"state":"all"}`, key, "1", 403)
	call(manager, managementPath, strings.Replace(detail, `"kb"`, `"foreign-kb"`, 1), key, "1", 403)
	if _, err := lifecycle.NewStore(db).Append(ctx, lifecycle.Event{EventID: "revised", TenantID: 7, KnowledgeBaseID: "kb", DocumentID: "doc", FromState: lifecycle.Active, ToState: "superseded", Reason: "revision_published", OccurredAt: now}); err != nil {
		t.Fatal(err)
	}
	call(manager, managementPath, detail, key, "1", 404)
	call(manager, managementPath, download, key, "1", 404)
	archivedDownload := strings.TrimSuffix(download, "}") + `,"state":"inactive"}`
	call(manager, managementPath, archivedDownload, key, "1", 403)
	if got := call(manager, managementPath, strings.TrimSuffix(archivedDownload, "}")+`,"can_write":true}`, key, "1", 200); !strings.Contains(got, "5 个工作日") || strings.Contains(got, "private-") {
		t.Fatal("authorized archive download returned an unexpected payload", got)
	}
	root := t.TempDir()
	scope := corrections.Scope{TenantID: 7, KnowledgeBaseID: "kb"}
	if err := service.Project(ctx, root, scope); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, "7", "kb", "doc", "v1", "feedback.md")
	if archived, err := os.ReadFile(file); err != nil || !strings.Contains(string(archived), "历史版本") || !strings.Contains(string(archived), "5 个工作日") || strings.Contains(string(archived), "private-") {
		t.Fatal("old revision did not keep its approved archived Markdown", err)
	}
	if err := db.Write(ctx, func(tx *sql.Tx) error {
		if _, err := tx.Exec("DELETE FROM ext_source_snapshot_evidence WHERE version_node_id='v1'"); err != nil {
			return err
		}
		_, err := tx.Exec("DELETE FROM ext_document_version WHERE version_node_id='v1'")
		return err
	}); err != nil {
		t.Fatal("correction history blocked cleanup of source metadata", err)
	}
	if err := service.Project(ctx, root, scope); err != nil {
		t.Fatal(err)
	}
	if cleaned, err := os.ReadFile(file); err != nil || strings.Contains(string(cleaned), "5 个工作日") {
		t.Fatal("cleaned version retained correction prose in managed Markdown", err)
	}
	native.EnabledKnowledgeBaseIDs = nil
	if got := call(bridgeHandler, bridgePath, discover, key, "1", 200); got != `{"sources":[]}` {
		t.Fatal("cleaned source remained discoverable", got)
	}
	call(manager, managementPath, detail, key, "1", 404)
	history := strings.TrimSuffix(detail, "}") + `,"state":"inactive","can_write":true}`
	if got := call(manager, managementPath, history, key, "1", 200); !strings.Contains(got, `"state":"inactive"`) {
		t.Fatal("cleanup removed minimum correction audit identity", got)
	}
	if err := db.Write(ctx, func(tx *sql.Tx) error {
		for i, action := range []string{"feedback_approve", "feedback_activate", "revision_split"} {
			if _, err := tx.Exec(`INSERT INTO ext_business_receipts VALUES('7','kb',?, 'hash','{}')`, action); err != nil {
				return err
			}
			if _, err := tx.Exec(`INSERT INTO ext_business_audit(tenant_id,knowledge_base_id,operation_id,actor_id,action,aggregate_id,revision,status,occurred_at,payload_json)
 VALUES('7','kb',?,'reviewer',?,'review',?,'recorded','2026-01-04T00:00:00Z','{"text":"private-audit-payload"}')`, action, action, i+2); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = store.OpenDurable(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	manager = management.New(db, key, []lifecycle.PolicyScope{{TenantID: 7, KnowledgeBaseID: "kb"}})
	for i, action := range []string{"feedback_approve", "feedback_activate"} {
		if i == 1 {
			audit = strings.TrimSuffix(audit, "}") + `,"page":2}`
		}
		got := call(manager, managementPath, audit, key, "1", 200)
		var report struct {
			Data corrections.HistoryReport `json:"data"`
		}
		if err := json.Unmarshal([]byte(got), &report); err != nil || report.Data.State != "inactive" || report.Data.ObservedAt.IsZero() ||
			len(report.Data.Events) != 1 || report.Data.Events[0].Action != action || report.Data.HasMore != (i == 0) || strings.Contains(got, "private-") {
			t.Fatal("wrong reconstructed state or audit page", got, err)
		}
	}
	call(manager, managementPath, strings.Replace(audit, `"v1"`, `"wrong-version"`, 1), key, "1", 404)
}
