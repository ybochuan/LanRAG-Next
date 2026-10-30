package ingest

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ybochuan/LanRAG-Next/extensions/weknora"
)

func TestHTTPSourceUsesNativePaginationAndValidatesScope(t *testing.T) {
	for _, scenario := range []string{"ok", "wrong_tenant", "wrong_kb", "wrong_page", "short_page", "missing_data"} {
		t.Run(scenario, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/v1/knowledge-bases/kb-a/knowledge" || r.URL.Query().Get("page") != "1" || r.URL.Query().Get("page_size") != "1" || r.URL.Query().Get("sort_order") != "asc" {
					t.Error("wrong listing request")
				}
				if scenario == "missing_data" {
					fmt.Fprint(w, `{"success":true,"total":0,"page":1,"page_size":1}`)
					return
				}
				tenant, kb, page := 7, "kb-a", 1
				if scenario == "wrong_tenant" {
					tenant = 8
				}
				if scenario == "wrong_kb" {
					kb = "other"
				}
				if scenario == "wrong_page" {
					page = 2
				}
				data := fmt.Sprintf(`[{"id":"doc-a","tenant_id":%d,"knowledge_base_id":%q,"parse_status":"completed","updated_at":"2026-10-30T00:00:00Z"}]`, tenant, kb)
				if scenario == "short_page" {
					data = "[]"
				}
				fmt.Fprintf(w, `{"success":true,"data":%s,"total":2,"page":%d,"page_size":1}`, data, page)
			}))
			defer s.Close()
			c, err := weknora.NewClient(s.URL, "secret", 7)
			if err != nil {
				t.Fatal(err)
			}
			source, err := NewHTTPSource(c, 1)
			if err != nil {
				t.Fatal(err)
			}
			page, err := source.List(context.Background(), Scope{7, "kb-a"}, 1)
			if scenario != "ok" {
				if err == nil {
					t.Fatal("invalid page accepted")
				}
				return
			}
			if err != nil || page.NextPage != 2 || len(page.Documents) != 1 || page.Documents[0].ParseStatus != "completed" {
				t.Fatalf("page=%+v %v", page, err)
			}
			if _, err := source.List(context.Background(), Scope{8, "kb-a"}, 1); err == nil {
				t.Fatal("cross tenant request allowed")
			}
		})
	}
}
