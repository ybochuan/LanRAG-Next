package taggov

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ybochuan/LanRAG-Next/extensions/weknora"
)

func TestHTTPStoreRefusesAmbiguousTagsAndSendsNativeIDArray(t *testing.T) {
	for _, scenario := range []string{"ok", "empty", "null", "missing", "wrong_scope"} {
		t.Run(scenario, func(t *testing.T) {
			var writes atomic.Int32
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/tags/ids") {
					w.WriteHeader(http.StatusNotFound)
					return
				}
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

// POST-M0-TAGS: the explicit tag read endpoint returns a real array, including
// [] for an untagged document, so null is never ambiguous. A cross-scope or
// malformed reply is refused instead of being read as an empty tag set.
func TestHTTPStoreExplicitTagReadEndpoint(t *testing.T) {
	for _, scenario := range []string{"one", "empty", "wrong_scope", "malformed"} {
		t.Run(scenario, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/v1/knowledge/doc-a/tags/ids" {
					t.Errorf("unexpected path: %s", r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
					return
				}
				data := map[string]any{"knowledge_id": "doc-a", "tenant_id": 7, "knowledge_base_id": "kb-a",
					"tags": []any{map[string]any{"id": "native-id", "name": "business:A", "tenant_id": 7, "knowledge_base_id": "kb-a"}}}
				switch scenario {
				case "empty":
					data["tags"] = []any{}
				case "wrong_scope":
					data["knowledge_base_id"] = "other"
				case "malformed":
					data["tags"] = []any{map[string]any{"id": "../escape", "name": "business:X", "tenant_id": 7, "knowledge_base_id": "kb-a"}}
				}
				json.NewEncoder(w).Encode(map[string]any{"success": true, "data": data})
			}))
			defer s.Close()
			client, err := weknora.NewClient(s.URL, "secret", 7)
			if err != nil {
				t.Fatal(err)
			}
			adapter := NewHTTPStore(client)
			tags, err := adapter.ReadTags(context.Background(), Scope{7, "kb-a", "doc-a"})
			switch scenario {
			case "one":
				if err != nil || len(tags) != 1 || tags[0].Name != "business:A" {
					t.Fatalf("one: %v %v", tags, err)
				}
			case "empty":
				if err != nil || tags == nil || len(tags) != 0 {
					t.Fatalf("empty: %v %v", tags, err)
				}
			default:
				if err == nil {
					t.Fatalf("%s should be refused", scenario)
				}
			}
		})
	}
}
