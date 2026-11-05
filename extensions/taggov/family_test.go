package taggov

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/lifecycle"
	"github.com/ybochuan/LanRAG-Next/extensions/outbox"
	"github.com/ybochuan/LanRAG-Next/extensions/weknora"
)

func TestFamilyProjectionReconcilesCatalogAndPreservesDocumentTags(t *testing.T) {
	ctx := context.Background()
	db := tagDatabase(t)
	now := time.Now().UTC()
	item := outbox.Item{ID: "family-intent", TenantID: 7, KnowledgeBaseID: "kb-a", ProjectionType: "family_tag", AggregateType: "document", AggregateID: "doc-a", CausationType: "document_version", CausationID: "version-target", Payload: json.RawMessage(`{"family_id":"stale-payload"}`), CreatedAt: now}
	if err := db.Write(ctx, func(tx *sql.Tx) error {
		if _, err := tx.Exec(`INSERT INTO ext_lineage_node VALUES('version-source','7','kb-a','document_version','doc-a','now');
INSERT INTO ext_document_version_family VALUES('family-real','7','kb-a','doc-a','','version-source','version-source',1,'now','now');
INSERT INTO ext_revision_proposals(tenant_id,knowledge_base_id,proposal_id,source_document_id,source_version_id,family_id,trigger_guidance_ids,proposed_patch,target_content_hash,status,revision,publication_key,target_document_id,created_at,published_at)
VALUES('7','kb-a','revision','doc-a','version-source','family-real','[]','body','hash','published',1,'key','doc-b','now','now');`); err != nil {
			return err
		}
		return outbox.EnqueueTx(ctx, tx, item)
	}); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	businessTag := nativeTag{ID: "user-tag", Name: "用户标签", TenantID: 7, KBID: "kb-a"}
	stateTag := nativeTag{ID: "state-tag", Name: "gov:state:superseded", TenantID: 7, KBID: "kb-a"}
	oldTag := nativeTag{ID: "old-tag", Name: "gov:family:old", TenantID: 7, KBID: "kb-a"}
	familyTag := nativeTag{ID: "native-family-tag", Name: "gov:family:family-real", TenantID: 7, KBID: "kb-a"}
	catalog := []nativeTag{businessTag, stateTag, oldTag}
	var docTags []nativeTag // Unknown native tag load on first attempt.
	creates, replaces := 0, 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch r.Method + " " + r.URL.Path {
		case "GET /api/v1/knowledge-bases/kb-a/tags":
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{"page": 1, "page_size": 50, "total": len(catalog), "data": catalog}})
		case "POST /api/v1/knowledge-bases/kb-a/tags":
			var req struct {
				Name string `json:"name"`
			}
			if json.NewDecoder(r.Body).Decode(&req) != nil || req.Name != familyTag.Name {
				t.Error("wrong tag created")
				w.WriteHeader(400)
				return
			}
			creates++
			catalog = append(catalog, familyTag)
			w.WriteHeader(503) // Commit succeeded; acknowledgment did not.
		case "GET /api/v1/knowledge/doc-a":
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{"id": "doc-a", "tenant_id": 7, "knowledge_base_id": "kb-a", "tags": docTags}})
		case "PUT /api/v1/knowledge/tags":
			var req struct {
				Tags []string `json:"tag_ids"`
			}
			if json.NewDecoder(r.Body).Decode(&req) != nil || !equalIDs(req.Tags, []string{businessTag.ID, stateTag.ID, familyTag.ID}) {
				t.Error("replacement lost existing tags or trusted an intent payload")
				w.WriteHeader(400)
				return
			}
			replaces++
			docTags = []nativeTag{businessTag, stateTag, familyTag}
			_ = json.NewEncoder(w).Encode(map[string]bool{"success": true})
		default:
			w.WriteHeader(404)
		}
	}))
	defer upstream.Close()
	client, err := weknora.NewClient(upstream.URL, "fixture-key", 7)
	if err != nil {
		t.Fatal(err)
	}
	api := NewHTTPStore(client)
	writer, err := NewWriter(api, 4)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	p := NewProjector(db, lifecycle.NewStore(db), NewMapping(db, api), writer)
	if err := p.Handle(ctx, item); !errors.Is(err, ErrAmbiguousTags) {
		t.Fatalf("unknown tag list must refuse document replacement: %v", err)
	}
	mu.Lock()
	if creates != 1 || replaces != 0 {
		t.Error("catalog create or null protection failed")
	}
	docTags = []nativeTag{businessTag, stateTag, oldTag}
	mu.Unlock()
	for range 2 {
		if err := p.Handle(ctx, item); err != nil {
			t.Fatal(err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if creates != 1 || replaces != 2 {
		t.Fatalf("non-idempotent reconciliation: creates=%d replacements=%d", creates, replaces)
	}
}
