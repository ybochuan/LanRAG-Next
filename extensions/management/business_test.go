package management

import (
	"database/sql"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/lifecycle"
)

func TestBusinessManagementUsesExistingAuthorityAndReceipts(t *testing.T) {
	s := fixture(t)
	key := s.key
	body := `{"action":"business_guidance_seed","tenant_id":7,"knowledge_base_id":"kb","actor_id":"7:owner","operation_id":"seed","business":{"payload":{"text":"Check effective date","topic":"date"}}}`
	if w := call(s, body, key); w.Code != 403 {
		t.Fatalf("readonly business mutation=%d %s", w.Code, w.Body.String())
	}
	var request map[string]any
	if e := json.Unmarshal([]byte(body), &request); e != nil {
		t.Fatal(e)
	}
	request["can_write"] = true
	encoded, _ := json.Marshal(request)
	w := call(s, string(encoded), key)
	if w.Code != 200 {
		t.Fatalf("business not wired: %d %s", w.Code, w.Body.String())
	}
	again := call(s, string(encoded), key)
	if again.Code != 200 || again.Body.String() != w.Body.String() {
		t.Fatal("unstable receipt")
	}
	request["tenant_id"] = 8
	encoded, _ = json.Marshal(request)
	if call(s, string(encoded), key).Code != 403 {
		t.Fatal("foreign tenant accepted")
	}
	if call(s, `{"action":"business_guidance_list","tenant_id":7,"knowledge_base_id":"kb","business":{}}`, "bad").Code != 401 {
		t.Fatal("credential bypass")
	}
}

func TestRelationManagementListIsScopedAndReadonly(t *testing.T) {
	s := fixture(t)
	w := call(s, `{"action":"business_relation_list","tenant_id":7,"knowledge_base_id":"kb","can_write":false}`, s.key)
	if w.Code != 200 {
		t.Fatalf("readonly relation list: %d %s", w.Code, w.Body.String())
	}
	if w = call(s, `{"action":"business_relation_list","tenant_id":8,"knowledge_base_id":"kb"}`, s.key); w.Code != 403 {
		t.Fatal("foreign scope read", w.Code)
	}
}

func TestWorkbenchSupplementRelationListAndDetail(t *testing.T) {
	s := fixture(t)
	ctx := t.Context()
	now := time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)
	for _, doc := range []string{"child", "parent"} {
		if _, err := lifecycle.NewStore(s.db).Append(ctx, lifecycle.Event{
			EventID: "event-" + doc, TenantID: 7, KnowledgeBaseID: "kb", DocumentID: doc,
			ToState: lifecycle.Active, Reason: "fixture", OccurredAt: now,
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.db.Write(ctx, func(tx *sql.Tx) error {
		stamp, hash := now.Format(time.RFC3339), strings.Repeat("a", 64)
		for _, doc := range []string{"child", "parent"} {
			node, family := "version-"+doc, "family-"+doc
			if _, err := tx.Exec(`INSERT INTO ext_lineage_node VALUES(?,'7','kb','document_version',?,?)`, node, doc, stamp); err != nil {
				return err
			}
			if _, err := tx.Exec(`INSERT INTO ext_document_version_family VALUES(?,'7','kb',?,'',?,?,1,?,?)`, family, doc, node, node, stamp, stamp); err != nil {
				return err
			}
			if _, err := tx.Exec(`INSERT INTO ext_document_version VALUES(?,'7','kb',?,?,?,1,?,'','fixture',?)`, node, family, doc, hash, "event-"+doc, stamp); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(`INSERT INTO ext_lineage_edge VALUES('edge','7','kb','version-child','version-parent','supplements','governance_critical',?)`, stamp); err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO ext_supplement_declarations VALUES('declaration','7','kb','child',1,'version-child','event-child','actor','declare',?,'active',?)`, hash, stamp); err != nil {
			return err
		}
		_, err := tx.Exec(`INSERT INTO ext_supplement_parents VALUES('declaration','7','kb','child','parent','version-parent','event-parent');
			INSERT INTO ext_supplement_current VALUES('7','kb','child',1,'declaration');
			INSERT INTO ext_supplement_edge_facts VALUES('declaration','7','kb','child','parent','edge');`)
		return err
	}); err != nil {
		t.Fatal(err)
	}

	for _, query := range []struct {
		name, action, state, document, id string
		want                              int
	}{
		{name: "legacy list", action: "relation_list"},
		{name: "workbench list", action: "relation_list", state: "workbench-relations", want: 1},
		{name: "type and status", action: "relation_list", state: "workbench-relations:supplements:supplement_declared", want: 1},
		{name: "global detail", action: "relation_list", state: "workbench-relations", id: "supplement:edge", want: 1},
		{name: "incoming endpoint", action: "related", state: "workbench-relations:supplements:", document: "parent", want: 1},
		{name: "scoped detail", action: "related", state: "workbench-relations", document: "parent", id: "supplement:edge", want: 1},
		{name: "unrelated document", action: "related", state: "workbench-relations", document: "other", id: "supplement:edge"},
	} {
		t.Run(query.name, func(t *testing.T) {
			body, err := json.Marshal(Request{
				Action: "business_" + query.action, TenantID: 7, KnowledgeBaseID: "kb",
				State: query.state, DocumentID: query.document, Business: json.RawMessage(`{"id":"` + query.id + `"}`),
			})
			if err != nil {
				t.Fatal(err)
			}
			w := call(s, string(body), s.key)
			var result struct {
				Data []struct {
					ID       string `json:"id"`
					EdgeType string `json:"edge_type"`
					Status   string `json:"status"`
				} `json:"data"`
			}
			if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil || len(result.Data) != query.want {
				t.Fatalf("want %d records, got HTTP %d %s", query.want, w.Code, w.Body.String())
			}
			if query.want == 1 {
				row := result.Data[0]
				if row.ID != "supplement:edge" || row.EdgeType != "supplements" || row.Status != "supplement_declared" {
					t.Fatalf("unexpected relation: %+v", row)
				}
			}
		})
	}
}
