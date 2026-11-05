package management

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/lifecycle"
)

func TestOverviewTitleFilterAndBusyFallback(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "native #1.db")
	native, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer native.Close()
	if _, err = native.Exec(`CREATE TABLE knowledges (id TEXT, tenant_id INTEGER, knowledge_base_id TEXT, title TEXT, deleted_at DATETIME);
INSERT INTO knowledges VALUES ('a',7,'kb','other',NULL),('b',7,'kb','历史ABC%',NULL),('c',8,'kb','历史ABC%',NULL)`); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"a", "b", "c"} {
		if _, err = lifecycle.NewStore(s.db).Append(ctx, lifecycle.Event{EventID: id, TenantID: 7, KnowledgeBaseID: "kb", DocumentID: id, ToState: lifecycle.Active, Reason: "test", OccurredAt: time.Now()}); err != nil {
			t.Fatal(err)
		}
	}
	calls := 0
	s.WithNativeSQLite(path, func(_ context.Context, _ string, ids []string) (map[string]string, error) {
		calls++
		return map[string]string{"a": "other", "b": "历史ABC%"}, nil
	})
	check := func(body string, total int, id string) {
		t.Helper()
		w := call(s, body, s.key)
		var out struct {
			Data struct {
				Total     int
				Documents []document
				Counts    map[string]int
			}
		}
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil || w.Code != 200 || out.Data.Total != total || out.Data.Counts["active"] != 3 || len(out.Data.Documents) != 1 || out.Data.Documents[0].DocumentID != id {
			t.Fatalf("unexpected overview: %d %s", w.Code, w.Body.String())
		}
		if id == "b" && out.Data.Documents[0].Title != "历史ABC%" {
			t.Fatal("native title missing")
		}
	}
	filtered := `{"action":"overview","tenant_id":7,"knowledge_base_id":"kb","state":"active","title":"abc%","page_size":1}`
	check(filtered, 1, "b")
	check(`{"action":"overview","tenant_id":7,"knowledge_base_id":"kb","page":3,"page_size":1}`, 3, "c")
	if calls != 0 {
		t.Fatal("fallback used on successful join")
	}
	if _, err = native.Exec("BEGIN EXCLUSIVE"); err != nil {
		t.Fatal(err)
	}
	defer native.Exec("ROLLBACK")
	check(filtered, 1, "b")
	if calls != 1 {
		t.Fatal("busy query did not use fallback")
	}
}
