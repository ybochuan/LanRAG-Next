package taggov

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/ybochuan/LanRAG-Next/extensions/weknora"
)

func TestHTTPStoreRefusesAmbiguousTagsAndSendsNativeIDArray(t *testing.T) {
	for _, scenario := range []string{"ok", "empty", "null", "missing", "wrong_scope"} {
		t.Run(scenario, func(t *testing.T) {
			var writes atomic.Int32
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "PUT" {
					writes.Add(1)
					var body struct {
						KB   string   `json:"kb_id"`
						IDs  []string `json:"knowledge_ids"`
						Tags []string `json:"tag_ids"`
					}
					if json.NewDecoder(r.Body).Decode(&body) != nil || body.KB != "kb-a" || len(body.IDs) != 1 || body.IDs[0] != "doc-a" || body.Tags == nil {
						t.Error("invalid native replace body")
					}
					fmt.Fprint(w, `{"success":true}`)
					return
				}
				data := map[string]any{"id": "doc-a", "tenant_id": 7, "knowledge_base_id": "kb-a", "tags": []any{map[string]any{"id": "native-id", "name": "business:A", "tenant_id": 7, "knowledge_base_id": "kb-a"}}}
				switch scenario {
				case "empty":
					data["tags"] = []any{}
				case "null":
					data["tags"] = nil
				case "missing":
					delete(data, "tags")
				case "wrong_scope":
					data["knowledge_base_id"] = "other"
				}
				json.NewEncoder(w).Encode(map[string]any{"success": true, "data": data})
			}))
			defer s.Close()
			client, err := weknora.NewClient(s.URL, "secret", 7)
			if err != nil {
				t.Fatal(err)
			}
			adapter := NewHTTPStore(client)
			scope := Scope{7, "kb-a", "doc-a"}
			tags, err := adapter.ReadTags(context.Background(), scope)
			if scenario == "null" || scenario == "missing" {
				if !errors.Is(err, ErrAmbiguousTags) || writes.Load() != 0 {
					t.Fatalf("ambiguous tags=%v writes=%d", err, writes.Load())
				}
				return
			}
			if scenario == "wrong_scope" {
				if err == nil {
					t.Fatal("cross-scope read allowed")
				}
				return
			}
			if err != nil || (scenario == "ok" && (len(tags) != 1 || tags[0].ID != "native-id")) {
				t.Fatalf("tags=%v %v", tags, err)
			}
			if err := adapter.ReplaceTags(context.Background(), scope, nil); err != nil || writes.Load() != 1 {
				t.Fatalf("replace=%v writes=%d", err, writes.Load())
			}
		})
	}
}
