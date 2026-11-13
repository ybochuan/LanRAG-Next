package mcp

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/ybochuan/LanRAG-Next/extensions/lifecycle"
	"github.com/ybochuan/LanRAG-Next/extensions/lineage"
	"github.com/ybochuan/LanRAG-Next/extensions/provenance"
	"github.com/ybochuan/LanRAG-Next/extensions/retrieval"
	"github.com/ybochuan/LanRAG-Next/extensions/store"
	"github.com/ybochuan/LanRAG-Next/extensions/weknora"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type relatedSnapshotFixture struct{}

func (relatedSnapshotFixture) Refresh(_ context.Context, _ provenance.Scope, id string) (lineage.Version, error) {
	return lineage.Version{ID: "version-" + id, DocumentID: id}, nil
}

func TestStreamableDiscoveryScopeAndLifecycle(t *testing.T) {
	ctx := context.Background()
	db, e := store.Open(ctx, filepath.Join(t.TempDir(), "ext.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	parentHash, childHash := strings.Repeat("a", 32), strings.Repeat("b", 32)
	importID, err := db.PrepareAttachmentImport(ctx, 7, "kb-a", "parent", parentHash, strings.Repeat("c", 64), childHash)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.ClaimAttachmentUpload(ctx, importID); err != nil {
		t.Fatal(err)
	}
	if err := db.BindAttachments(ctx, 7, "kb-a", "parent", parentHash, []store.AttachmentBinding{{ImportID: importID, AttachmentID: "child"}}); err != nil {
		t.Fatal(err)
	}
	docs := []any{
		map[string]any{"id": "parent", "tenant_id": 7, "knowledge_base_id": "kb-a", "title": "PARENT_ONLY_CANARY", "file_name": "parent.pdf", "file_hash": parentHash},
		map[string]any{"id": "notice", "tenant_id": 7, "knowledge_base_id": "kb-a", "file_name": "supplement.pdf"},
		map[string]any{"id": "child", "tenant_id": 7, "knowledge_base_id": "kb-a", "file_name": "form.pdf", "file_hash": childHash, "metadata": map[string]string{"is_attachment": "true", "parent_document_id": "parent", "attachment_import_id": importID}},
		map[string]any{"id": "legacy", "tenant_id": 7, "knowledge_base_id": "kb-a", "file_name": "legacy.pdf", "metadata": map[string]string{"is_attachment": "true", "parent_document_id": "parent"}},
		map[string]any{"id": "partner", "tenant_id": 7, "knowledge_base_id": "kb-b", "file_name": "partner.pdf"},
		map[string]any{"id": "blocked", "tenant_id": 7, "knowledge_base_id": "kb-a", "file_name": "blocked.pdf"},
	}
	native := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rows := docs
		if r.URL.Path == "/api/v1/knowledge/batch" {
			rows = []any{}
			for _, id := range r.URL.Query()["ids"] {
				for _, d := range docs {
					if d.(map[string]any)["id"] == id {
						rows = append(rows, d)
					}
				}
			}
		}
		if strings.HasPrefix(r.URL.Path, "/api/v1/knowledge-bases/") && strings.HasSuffix(r.URL.Path, "/knowledge") {
			kb := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/v1/knowledge-bases/"), "/knowledge")
			rows = []any{}
			for _, d := range docs {
				if d.(map[string]any)["knowledge_base_id"] == kb {
					rows = append(rows, d)
				}
			}
		}
		json.NewEncoder(w).Encode(map[string]any{"success": true, "data": rows, "total": len(rows), "page": 1, "page_size": 100})
	}))
	defer native.Close()
	for _, id := range []string{"parent", "child", "legacy", "notice", "blocked"} {
		_, e = lifecycle.NewStore(db).Append(ctx, lifecycle.Event{EventID: id, TenantID: 7, KnowledgeBaseID: "kb-a", DocumentID: id, ToState: lifecycle.Active, Reason: "test", OccurredAt: time.Now()})
		if e != nil {
			t.Fatal(e)
		}
	}
	if _, e = lifecycle.NewStore(db).Append(ctx, lifecycle.Event{EventID: "blocked-stale", TenantID: 7, KnowledgeBaseID: "kb-a", DocumentID: "blocked", FromState: lifecycle.Active, ToState: lifecycle.Stale, Reason: "test", OccurredAt: time.Now()}); e != nil {
		t.Fatal(e)
	}
	_, e = lifecycle.NewStore(db).Append(ctx, lifecycle.Event{EventID: "partner", TenantID: 7, KnowledgeBaseID: "kb-b", DocumentID: "partner", ToState: lifecycle.Active, Reason: "test", OccurredAt: time.Now()})
	if e != nil {
		t.Fatal(e)
	}
	if err := db.Write(ctx, func(tx *sql.Tx) error {
		const stamp = "2026-11-01T00:00:00Z"
		for _, id := range []string{"parent", "notice"} {
			node, family := "version-"+id, "family-"+id
			if _, err := tx.Exec(`INSERT INTO ext_lineage_node VALUES(?,'7','kb-a','document_version',?,?)`, node, id, stamp); err != nil {
				return err
			}
			if _, err := tx.Exec(`INSERT INTO ext_document_version_family VALUES(?,'7','kb-a',?,'',?,?,1,?,?)`, family, id, node, node, stamp, stamp); err != nil {
				return err
			}
			if _, err := tx.Exec(`INSERT INTO ext_document_version VALUES(?,'7','kb-a',?,?,?,1,?,'','fixture',?)`, node, family, id, strings.Repeat("d", 64), id, stamp); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(`INSERT INTO ext_lineage_node VALUES('version-partner','7','kb-b','document_version','partner',?)`, stamp); err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO ext_document_version_family VALUES('family-partner','7','kb-b','partner','','version-partner','version-partner',1,?,?)`, stamp, stamp); err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO ext_document_version VALUES('version-partner','7','kb-b','family-partner','partner',?,1,'partner','','fixture',?)`, strings.Repeat("f", 64), stamp); err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO ext_lineage_node VALUES('version-blocked','7','kb-a','document_version','blocked',?)`, stamp); err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO ext_document_version_family VALUES('family-blocked','7','kb-a','blocked','','version-blocked','version-blocked',1,?,?)`, stamp, stamp); err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO ext_document_version VALUES('version-blocked','7','kb-a','family-blocked','blocked',?,1,'blocked','','fixture',?)`, strings.Repeat("9", 64), stamp); err != nil {
			return err
		}
		_, err := tx.Exec(`INSERT INTO ext_lineage_edge VALUES('edge','7','kb-a','version-notice','version-parent','supplements','governance_critical','2026-11-01T00:00:00Z');
			INSERT INTO ext_supplement_declarations VALUES('declaration','7','kb-a','notice',1,'version-notice','notice','actor','op',?,'active','2026-11-01T00:00:00Z');
			INSERT INTO ext_supplement_parents VALUES('declaration','7','kb-a','notice','parent','version-parent','parent');
			INSERT INTO ext_supplement_current VALUES('7','kb-a','notice',1,'declaration');
			INSERT INTO ext_supplement_edge_facts VALUES('declaration','7','kb-a','notice','parent','edge');
			INSERT INTO ext_document_relations(tenant_id,knowledge_base_id,relation_id,document_id,target_knowledge_base_id,target_document_id,source_version_id,target_version_id,edge_type,status,confidence,revision)
			VALUES('7','kb-a','related-parent-notice','parent','kb-a','notice','version-parent','version-notice','related_to','active',0.8,1);
			INSERT INTO ext_document_relations(tenant_id,knowledge_base_id,relation_id,document_id,target_knowledge_base_id,target_document_id,source_version_id,target_version_id,edge_type,status,confidence,revision)
			VALUES('7','kb-a','related-parent-partner','parent','kb-b','partner','version-parent','version-partner','related_to','active',0.8,1);
			INSERT INTO ext_document_relations(tenant_id,knowledge_base_id,relation_id,document_id,target_knowledge_base_id,target_document_id,source_version_id,target_version_id,edge_type,status,confidence,revision)
			VALUES('7','kb-a','related-parent-blocked','parent','kb-a','blocked','version-parent','version-blocked','related_to','active',0.8,1);
			INSERT INTO ext_business_document_families VALUES('7','kb-a','family-related','测试族',1,?,?);
			INSERT INTO ext_document_relations(tenant_id,knowledge_base_id,relation_id,document_id,target_knowledge_base_id,target_document_id,source_version_id,target_version_id,edge_type,status,confidence,revision)
			VALUES('7','kb-a','related-parent-notice-low','parent','kb-a','notice','version-parent','version-notice','related_to','active',0.4,1);
			INSERT INTO ext_family_suggestions VALUES('7','kb-a','suggest-notice-low','family-related','relationship','parent','notice','related_to','version-parent','version-notice','{}',0.4,0,'confirmed',1,'related-parent-notice-low')`, strings.Repeat("e", 64), stamp, stamp)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	c, _ := weknora.NewClient(native.URL, "test", 7)
	p, _ := retrieval.NewPolicy(db, 7, []string{"kb-a", "kb-b"})
	p.WithAttachments(c).WithSources(relatedSnapshotFixture{})
	s := &Server{DB: db, Client: c, Policy: p, Key: strings.Repeat("k", 32), Tenant: 7, KBs: []string{"kb-a", "kb-b"}}
	endpoint := httptest.NewServer(s)
	defer endpoint.Close()
	call := func(body string, key bool) *httptest.ResponseRecorder {
		r, err := http.NewRequest("POST", endpoint.URL+"/mcp", bytes.NewBufferString(body))
		if err != nil {
			t.Fatal(err)
		}
		if key {
			r.Header.Set("Authorization", "Bearer "+s.Key)
		}
		w := httptest.NewRecorder()
		response, err := endpoint.Client().Do(r)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		w.WriteHeader(response.StatusCode)
		io.Copy(w.Body, response.Body)
		return w
	}
	if !strings.Contains(call(`{"jsonrpc":"2.0","id":0,"method":"initialize","params":{"protocolVersion":"2025-03-26"}}`, true).Body.String(), `"protocolVersion":"2025-03-26"`) {
		t.Fatal("initialization failed")
	}
	list := `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`
	if call(list, false).Code != 401 || !strings.Contains(call(list, true).Body.String(), "query_related_docs") {
		t.Fatal("auth/discovery failed")
	}
	request := func(id string) string {
		return fmt.Sprintf(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"query_related_docs","arguments":{"document_id":%q,"_native_scope":{"tenant_id":7,"knowledge_base_ids":["kb-a"],"knowledge_ids":["parent"]}}}}`, id)
	}
	scoped := call(request("parent"), true).Body.String()
	if !strings.Contains(scoped, "/api/v1/knowledge/child/download") {
		t.Fatal("attachment link missing")
	}
	if strings.Contains(scoped, "supplement.pdf") || strings.Contains(scoped, "legacy.pdf") || strings.Contains(scoped, `"edge_type":"related_to"`) {
		t.Fatal("related metadata escaped document-only authorization")
	}
	broad := strings.Replace(request("parent"), `"knowledge_ids":["parent"]`, `"whole_kb_ids":["kb-a"]`, 1)
	if got := call(broad, true).Body.String(); !strings.Contains(got, "supplement.pdf") || !strings.Contains(got, "legacy.pdf") {
		t.Fatal("authorized same-library related documents missing")
	}
	if got := call(broad, true).Body.String(); !strings.Contains(got, `"edge_type":"related_to"`) || !strings.Contains(got, `"document_id":"notice"`) {
		t.Fatal("confirmed relation missing from broad query")
	}
	if got := call(broad, true).Body.String(); !strings.Contains(got, `"confidence":0.4`) {
		t.Fatal("confirmed family-suggestion relation missing from broad query")
	}
	if got := call(broad, true).Body.String(); strings.Contains(got, `"document_id":"blocked"`) {
		t.Fatal("stale relation target leaked from broad query")
	}
	cross := strings.Replace(strings.Replace(request("parent"), `"knowledge_base_ids":["kb-a"]`, `"knowledge_base_ids":["kb-a","kb-b"]`, 1), `"knowledge_ids":["parent"]`, `"whole_kb_ids":["kb-a","kb-b"]`, 1)
	if got := call(cross, true).Body.String(); !strings.Contains(got, `"knowledge_base_id":"kb-b"`) || !strings.Contains(got, `"document_id":"partner"`) {
		t.Fatal("cross-KB relation missing from broad query")
	}
	if !strings.Contains(call(request("child"), true).Body.String(), `"isError":true`) {
		t.Fatal("native scope bypass")
	}
	childOnly := strings.Replace(request("child"), `"knowledge_ids":["parent"]`, `"knowledge_ids":["child"]`, 1)
	if got := call(childOnly, true).Body.String(); !strings.Contains(got, "/api/v1/knowledge/child/download") ||
		strings.Contains(got, "PARENT_ONLY_CANARY") {
		t.Fatal("child-only access omitted its link or leaked parent text")
	}
	_, e = lifecycle.NewStore(db).Append(ctx, lifecycle.Event{EventID: "expired", TenantID: 7, KnowledgeBaseID: "kb-a", DocumentID: "parent", FromState: lifecycle.Active, ToState: lifecycle.Stale, Reason: "expired", OccurredAt: time.Now()})
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(call(request("parent"), true).Body.String(), `"isError":true`) {
		t.Fatal("stale parent leaked related documents")
	}
}
