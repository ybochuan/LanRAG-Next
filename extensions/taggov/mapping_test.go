package taggov

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ybochuan/LanRAG-Next/extensions/store"
	"github.com/ybochuan/LanRAG-Next/extensions/weknora"
)

func tagDatabase(t *testing.T) *store.DB {
	t.Helper()
	db, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "agentrag_ext.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	return db
}

func TestCatalogReadsSecondPageAndRejectsAmbiguity(t *testing.T) {
	for _, scenario := range []string{"ok", "duplicate_name", "wrong_scope", "changed_total"} {
		t.Run(scenario, func(t *testing.T) {
			var pages atomic.Int32
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				pages.Add(1)
				page := 1
				if r.URL.Query().Get("page") == "2" {
					page = 2
				}
				var tags []map[string]any
				start, end := 0, 50
				if page == 2 {
					start, end = 50, 51
				}
				for i := start; i < end; i++ {
					name := fmt.Sprintf("business:%d", i)
					if i == 50 {
						name = "gov:state:active"
					}
					if scenario == "duplicate_name" && i == 50 {
						name = "business:0"
					}
					kb := "kb-a"
					if scenario == "wrong_scope" && i == 50 {
						kb = "other"
					}
					tags = append(tags, map[string]any{"id": fmt.Sprintf("native-%d", i), "name": name, "tenant_id": 7, "knowledge_base_id": kb})
				}
				total := 51
				if scenario == "changed_total" && page == 2 {
					total = 52
				}
				json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{"total": total, "page": page, "page_size": 50, "data": tags}})
			}))
			defer s.Close()
			c, err := weknora.NewClient(s.URL, "secret", 7)
			if err != nil {
				t.Fatal(err)
			}
			tag, err := NewMapping(tagDatabase(t), NewHTTPStore(c)).Resolve(context.Background(), Scope{7, "kb-a", "doc-a"}, "gov:state:active")
			if scenario == "ok" {
				if err != nil || tag.ID != "native-50" || pages.Load() != 2 {
					t.Fatalf("second page=%v %v calls=%d", tag, err, pages.Load())
				}
			} else if err == nil {
				t.Fatal("invalid catalog accepted")
			}
		})
	}
}

func TestMappingUsesKBLocalNativeIDAndPersistsIt(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		kb := "kb-a"
		if strings.Contains(r.URL.Path, "kb-b") {
			kb = "kb-b"
		}
		fmt.Fprintf(w, `{"success":true,"data":{"total":1,"page":1,"page_size":50,"data":[{"id":%q,"name":"gov:state:active","tenant_id":7,"knowledge_base_id":%q}]}}`, kb+"-native-id", kb)
	}))
	defer s.Close()
	c, err := weknora.NewClient(s.URL, "secret", 7)
	if err != nil {
		t.Fatal(err)
	}
	db := tagDatabase(t)
	m := NewMapping(db, NewHTTPStore(c))
	for _, kb := range []string{"kb-a", "kb-b"} {
		tag, err := m.Resolve(context.Background(), Scope{7, kb, "doc-a"}, "gov:state:active")
		if err != nil || tag.ID != kb+"-native-id" {
			t.Fatalf("mapping=%v %v", tag, err)
		}
		if err := db.View(context.Background(), func(tx *sql.Tx) error {
			var id string
			if err := tx.QueryRow("SELECT tag_id FROM ext_kb_tag_mapping WHERE tenant_id='7' AND knowledge_base_id=? AND canonical_name='gov:state:active'", kb).Scan(&id); err != nil {
				return err
			}
			if id != tag.ID {
				t.Error("mapping not durable")
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
}
