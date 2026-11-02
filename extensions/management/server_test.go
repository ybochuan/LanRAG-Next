package management

import (
	"context"
	"encoding/json"
	"github.com/ybochuan/LanRAG-Next/extensions/lifecycle"
	"github.com/ybochuan/LanRAG-Next/extensions/store"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func fixture(t *testing.T) *Server {
	t.Helper()
	db, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "ext.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return New(db, "management-test-credential-32-bytes", []lifecycle.PolicyScope{{TenantID: 7, KnowledgeBaseID: "kb"}})
}
func call(s *Server, body, key string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", "/management/lifecycle", strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+key)
	r.Header.Set("X-AgentRAG-Bridge-Version", "1")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}
func TestManagementRejectsCredentialsScopeAndReadonly(t *testing.T) {
	s := fixture(t)
	key := "management-test-credential-32-bytes"
	for _, test := range []struct {
		body, key string
		status    int
	}{
		{`{"action":"overview","tenant_id":7,"knowledge_base_id":"kb"}`, "wrong", 401},
		{`{"action":"overview","tenant_id":8,"knowledge_base_id":"kb"}`, key, 403},
		{`{"action":"put_rules","tenant_id":7,"knowledge_base_id":"kb","rules":{}}`, key, 403},
		{`{"action":"put_supplements","tenant_id":7,"knowledge_base_id":"kb","document_id":"doc","parents":[]}`, key, 403},
		{`{"action":"overview","tenant_id":7,"knowledge_base_id":"kb","unknown":true}`, key, 400},
	} {
		if w := call(s, test.body, test.key); w.Code != test.status {
			t.Fatalf("status=%d want=%d", w.Code, test.status)
		}
	}
}

func TestManagementSupplementReadAndUnavailableWriter(t *testing.T) {
	s := fixture(t)
	if _, e := lifecycle.NewStore(s.db).Append(context.Background(), lifecycle.Event{EventID: "doc", TenantID: 7, KnowledgeBaseID: "kb", DocumentID: "doc", ToState: lifecycle.Active, Reason: "fixture", OccurredAt: time.Now()}); e != nil {
		t.Fatal(e)
	}
	w := call(s, `{"action":"supplements","tenant_id":7,"knowledge_base_id":"kb","document_id":"doc"}`, s.key)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	w = call(s, `{"action":"put_supplements","tenant_id":7,"knowledge_base_id":"kb","document_id":"doc","can_write":true,"operation_id":"op","actor_id":"native-actor","parents":[]}`, s.key)
	if w.Code != 503 {
		t.Fatal("missing trusted registry must be unavailable", w.Code, w.Body.String())
	}
}

func TestManagementDisabledAndUnsupportedVersion(t *testing.T) {
	s := fixture(t)
	for _, version := range []string{"", "2"} {
		r := httptest.NewRequest("POST", "/management/lifecycle", strings.NewReader(`{"action":"overview","tenant_id":7,"knowledge_base_id":"kb"}`))
		r.Header.Set("Authorization", "Bearer management-test-credential-32-bytes")
		r.Header.Set("X-AgentRAG-Bridge-Version", version)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != 400 {
			t.Fatalf("version status=%d", w.Code)
		}
	}
	w := call(New(nil, "", nil), `{}`, "")
	if w.Code != 503 {
		t.Fatalf("disabled status=%d", w.Code)
	}
}
func TestManagementConcurrentRulesOneWinner(t *testing.T) {
	s := fixture(t)
	var wg sync.WaitGroup
	codes := make(chan int, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes <- call(s, `{"action":"put_rules","tenant_id":7,"knowledge_base_id":"kb","can_write":true,"expected_revision":0,"rules":{"valid_days":3}}`, s.key).Code
		}()
	}
	wg.Wait()
	close(codes)
	ok, conflict := 0, 0
	for code := range codes {
		if code == 200 {
			ok++
		} else if code == 409 {
			conflict++
		} else {
			t.Fatalf("status=%d", code)
		}
	}
	if ok != 1 || conflict != 11 {
		t.Fatalf("ok=%d conflict=%d", ok, conflict)
	}
}
func TestManagementDatesPreserveLabelsAndExpire(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	scope := lifecycle.Scope{TenantID: 7, KnowledgeBaseID: "kb", DocumentID: "doc"}
	m := lifecycle.NewMachine(s.db, time.Now)
	if _, err := lifecycle.NewStore(s.db).Append(ctx, lifecycle.Event{EventID: "seed", TenantID: 7, KnowledgeBaseID: "kb", DocumentID: "doc", ToState: lifecycle.Active, OccurredAt: time.Now(), Reason: "fixture"}); err != nil {
		t.Fatal(err)
	}

	w := call(s, `{"action":"put_annotations","tenant_id":7,"knowledge_base_id":"kb","document_id":"doc","can_write":true,"expected_revision":0,"annotations":{"effective_until":"2000-01-01T00:00:00Z","labels":{"classification":"policy"}}}`, s.key)
	if w.Code != 200 {
		t.Fatalf("save: %d %s", w.Code, w.Body.String())
	}
	snapshot, err := m.Snapshot(ctx, scope)
	if err != nil || snapshot.Current.State != lifecycle.Stale || snapshot.Annotations.Labels["classification"] != "policy" {
		t.Fatalf("snapshot=%+v err=%v", snapshot, err)
	}
	var data map[string]any
	if json.Unmarshal(w.Body.Bytes(), &data) != nil {
		t.Fatal("invalid response")
	}
	w = call(s, `{"action":"put_annotations","tenant_id":7,"knowledge_base_id":"kb","document_id":"doc","can_write":true,"expected_revision":1,"annotations":{"labels":{"classification":"policy"}}}`, s.key)
	snapshot, _ = m.Snapshot(ctx, scope)
	if w.Code != 200 || snapshot.Current.State != lifecycle.Stale {
		t.Fatal("clearing expiry revived document")
	}
}
