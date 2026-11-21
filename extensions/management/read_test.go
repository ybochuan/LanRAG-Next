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

func TestOverviewAttachmentStateFollowsParent(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "native.db")
	native, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer native.Close()
	if _, err = native.Exec("CREATE TABLE knowledges (id TEXT, tenant_id INTEGER, knowledge_base_id TEXT, title TEXT, deleted_at DATETIME); INSERT INTO knowledges VALUES ('parent',7,'kb','Main document',NULL),('child',7,'kb','annex-01.docx',NULL)"); err != nil {
		t.Fatal(err)
	}
	store := lifecycle.NewStore(s.db)
	for _, event := range []lifecycle.Event{
		{EventID: "parent-active", TenantID: 7, KnowledgeBaseID: "kb", DocumentID: "parent", ToState: lifecycle.Active, Reason: "fixture", OccurredAt: time.Now()},
		{EventID: "parent-superseded", TenantID: 7, KnowledgeBaseID: "kb", DocumentID: "parent", FromState: lifecycle.Active, ToState: "superseded", Reason: "family_replacement_effective", OccurredAt: time.Now().Add(time.Second)},
		{EventID: "child-active", TenantID: 7, KnowledgeBaseID: "kb", DocumentID: "child", ToState: lifecycle.Active, Reason: "fixture", OccurredAt: time.Now()},
		{EventID: "child-stale", TenantID: 7, KnowledgeBaseID: "kb", DocumentID: "child", FromState: lifecycle.Active, ToState: lifecycle.Stale, Reason: "expired", OccurredAt: time.Now().Add(time.Second)},
	} {
		if _, err = store.Append(ctx, event); err != nil {
			t.Fatal(err)
		}
	}
	if err = s.db.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO ext_attachment_links(import_id,tenant_id,knowledge_base_id,parent_document_id,parent_file_hash,attachment_sha256,attachment_file_hash,attachment_id,submitted,created_at) VALUES(?,?,?,?,?,?,?,?,?,?)`,
			"import-child", "7", "kb", "parent", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", "cccccccccccccccccccccccccccccccc", "child", 1, time.Now().UTC().Format(time.RFC3339Nano))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	s.WithNativeSQLite(path, nil)
	w := call(s, `{"action":"overview","tenant_id":7,"knowledge_base_id":"kb","page_size":20}`, s.key)
	var out struct {
		Data struct {
			Total     int
			Documents []document
			Counts    map[string]int
		}
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil || w.Code != 200 {
		t.Fatalf("overview=%d %s", w.Code, w.Body.String())
	}
	if out.Data.Counts["superseded"] != 2 || out.Data.Counts["active"] != 0 {
		t.Fatalf("counts=%+v", out.Data.Counts)
	}
	found := false
	for _, doc := range out.Data.Documents {
		if doc.DocumentID == "child" {
			found = true
			if doc.State != "superseded" {
				t.Fatalf("child state=%q, want superseded", doc.State)
			}
		}
	}
	if !found {
		t.Fatalf("child missing from page: %+v", out.Data.Documents)
	}
	w = call(s, `{"action":"overview","tenant_id":7,"knowledge_base_id":"kb","state":"superseded","page_size":20}`, s.key)
	var filtered struct {
		Data struct {
			Total int
		}
	}
	if err := json.Unmarshal(w.Body.Bytes(), &filtered); err != nil || w.Code != 200 || filtered.Data.Total != 2 {
		t.Fatalf("filtered overview=%d %s", w.Code, w.Body.String())
	}
}

func TestOverviewUsesNativeLiveDocumentSet(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "native.db")
	native, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer native.Close()
	if _, err = native.Exec("CREATE TABLE knowledges (id TEXT, tenant_id INTEGER, knowledge_base_id TEXT, title TEXT, deleted_at DATETIME); INSERT INTO knowledges VALUES ('live',7,'kb','Live document',NULL),('gone',7,'kb','Deleted document','2026-11-01T00:00:00Z')"); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"live", "gone"} {
		if _, err = lifecycle.NewStore(s.db).Append(ctx, lifecycle.Event{EventID: id, TenantID: 7, KnowledgeBaseID: "kb", DocumentID: id, ToState: lifecycle.Active, Reason: "test", OccurredAt: time.Now()}); err != nil {
			t.Fatal(err)
		}
	}
	s.WithNativeSQLite(path, nil)
	w := call(s, `{"action":"overview","tenant_id":7,"knowledge_base_id":"kb","page":1,"page_size":20}`, s.key)
	var out struct {
		Data struct {
			Total     int
			Documents []document
			Counts    map[string]int
		}
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil || w.Code != 200 {
		t.Fatalf("overview=%d %s", w.Code, w.Body.String())
	}
	if out.Data.Total != 1 || len(out.Data.Documents) != 1 || out.Data.Documents[0].DocumentID != "live" || out.Data.Documents[0].Title != "Live document" {
		t.Fatalf("deleted document leaked: %+v", out.Data)
	}
	if out.Data.Counts["active"] != 1 {
		t.Fatalf("counts=%+v", out.Data.Counts)
	}
}

func TestOverviewCannotOpenNativeFallsBack(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	for _, id := range []string{"a", "b"} {
		if _, err := lifecycle.NewStore(s.db).Append(ctx, lifecycle.Event{EventID: id, TenantID: 7, KnowledgeBaseID: "kb", DocumentID: id, ToState: lifecycle.Active, Reason: "test", OccurredAt: time.Now()}); err != nil {
			t.Fatal(err)
		}
	}
	s.WithNativeSQLite(filepath.Join(t.TempDir(), "missing-native.db"), func(_ context.Context, _ string, _ []string) (map[string]string, error) {
		return map[string]string{"a": "Alpha", "b": "Beta"}, nil
	})
	w := call(s, `{"action":"overview","tenant_id":7,"knowledge_base_id":"kb","page_size":20}`, s.key)
	var out struct {
		Data struct {
			Total          int
			Documents      []document
			Degraded       bool   `json:"degraded"`
			DegradedReason string `json:"degraded_reason"`
		}
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil || w.Code != 200 {
		t.Fatalf("unexpected overview: %d %s", w.Code, w.Body.String())
	}
	if !out.Data.Degraded || out.Data.DegradedReason != "native_database_unavailable" {
		t.Fatalf("expected degraded fallback, got %+v", out.Data)
	}
	if out.Data.Total != 2 || len(out.Data.Documents) != 2 {
		t.Fatalf("unexpected fallback page: %+v", out.Data)
	}
	if out.Data.Documents[0].Title != "Alpha" {
		t.Fatalf("fallback title = %q", out.Data.Documents[0].Title)
	}
}

func TestOverviewExcludesInconsistentCurrent(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	for _, id := range []string{"good", "bad"} {
		if _, err := lifecycle.NewStore(s.db).Append(ctx, lifecycle.Event{EventID: id, TenantID: 7, KnowledgeBaseID: "kb", DocumentID: id, ToState: lifecycle.Active, Reason: "test", OccurredAt: time.Now()}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.db.Write(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DROP TRIGGER IF EXISTS ext_lifecycle_current_update_matches_event`); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DROP TRIGGER IF EXISTS ext_lifecycle_current_update_heads_latest_event`); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `UPDATE ext_lifecycle_current SET state='stale' WHERE tenant_id=7 AND knowledge_base_id='kb' AND document_id='bad'`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	w := call(s, `{"action":"overview","tenant_id":7,"knowledge_base_id":"kb"}`, s.key)
	var out struct {
		Data struct {
			Total          int
			Counts         map[string]int
			Documents      []document
			Degraded       bool   `json:"degraded"`
			DegradedReason string `json:"degraded_reason"`
		}
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil || w.Code != 200 {
		t.Fatalf("unexpected overview: %d %s", w.Code, w.Body.String())
	}
	if out.Data.Total != 1 || len(out.Data.Documents) != 1 || out.Data.Documents[0].DocumentID != "good" {
		t.Fatalf("inconsistent current leaked into page: %+v", out.Data)
	}
	if out.Data.Counts["active"] != 1 || out.Data.Counts["stale"] != 0 {
		t.Fatalf("inconsistent current leaked into counts: %+v", out.Data.Counts)
	}
	if !out.Data.Degraded || out.Data.DegradedReason != "lifecycle_inconsistent" {
		t.Fatalf("expected lifecycle inconsistency degradation, got %+v", out.Data)
	}
}

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
INSERT INTO knowledges VALUES ('a',7,'kb','other',NULL),('b',7,'kb','历史ABC%',NULL),('c',7,'kb','历史XYZ%',NULL)`); err != nil {
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
