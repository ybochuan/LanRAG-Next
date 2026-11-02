package business

import (
	"context"
	"database/sql"
	"encoding/json"
	"github.com/ybochuan/LanRAG-Next/extensions/provenance"
	"github.com/ybochuan/LanRAG-Next/extensions/weknora"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestNativePublisherReconcilesExactMarkerAndContent(t *testing.T) {
	var creates atomic.Int32
	var exists atomic.Bool
	var badPage atomic.Bool
	var binary atomic.Bool
	p := Publication{Scope: provenance.Scope{TenantID: 7, KnowledgeBaseID: "kb"}, SourceDocumentID: "source", Key: textHash("publication"), Content: "replacement\n", ContentHash: textHash("replacement\n")}
	doc := map[string]any{"id": "target", "tenant_id": 7, "knowledge_base_id": "kb", "type": "manual", "title": "AgentRAG revision " + p.Key, "parse_status": "completed", "metadata": map[string]any{"content": p.Content, "status": "publish", "format": "markdown"}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-Key") != "test-api-key" {
			w.WriteHeader(401)
			return
		}
		switch {
		case r.URL.Path == "/api/v1/knowledge/source":
			typ := "manual"
			if binary.Load() {
				typ = "file"
			}
			json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{"id": "source", "tenant_id": 7, "knowledge_base_id": "kb", "type": typ, "metadata": map[string]string{"status": "publish"}}})
		case r.Method == "POST":
			creates.Add(1)
			exists.Store(true)
			json.NewEncoder(w).Encode(map[string]any{"success": true, "data": doc})
		case r.URL.Path == "/api/v1/knowledge/target":
			json.NewEncoder(w).Encode(map[string]any{"success": true, "data": doc})
		default:
			data := []any{}
			if exists.Load() {
				data = append(data, doc)
			}
			total := len(data)
			if badPage.Load() {
				total++
			}
			json.NewEncoder(w).Encode(map[string]any{"success": true, "data": data, "total": total, "page": 1, "page_size": 100})
		}
	}))
	defer server.Close()
	client, e := weknora.NewClient(server.URL, "test-api-key", 7)
	if e != nil {
		t.Fatal(e)
	}
	remote := NewNativePublisher(client)
	got, e := remote.Reconcile(context.Background(), p)
	if e != nil || got.ID != "" {
		t.Fatal(got, e)
	}
	got, e = remote.Create(context.Background(), p)
	if e != nil || !got.Indexed || got.ContentHash != p.ContentHash {
		t.Fatal(got, e)
	}
	got, e = remote.Reconcile(context.Background(), p)
	if e != nil || got.ID != "target" || creates.Load() != 1 {
		t.Fatal(got, e, creates.Load())
	}
	badPage.Store(true)
	if _, e = remote.Reconcile(context.Background(), p); e == nil {
		t.Fatal("partial remote listing treated as authoritative absence")
	}
	badPage.Store(false)
	binary.Store(true)
	if _, e = remote.Create(context.Background(), p); e == nil || creates.Load() != 1 {
		t.Fatal("binary source was automatically published", e, creates.Load())
	}
}
func TestLowRiskCoolingAndNegativeTopicCircuit(t *testing.T) {
	s, db, clock := fixture(t)
	ctx := context.Background()
	c := cmd("feedback_create", "low")
	c.Payload = Payload{Text: "Check dates", Topic: "date", Risk: "low"}
	f, e := s.Execute(ctx, c)
	if e != nil || f.Status != "approved" {
		t.Fatal("low risk fast path", f, e)
	}
	*clock = clock.Add(25 * time.Hour)
	c = cmd("feedback_activate", "activate")
	c.ID = f.ID
	c.ExpectedRevision = f.Revision
	if _, e = s.Execute(ctx, c); e != nil {
		t.Fatal(e)
	}
	for _, op := range []string{"negative1", "negative2"} {
		c = cmd("feedback_create", op)
		c.Payload = Payload{Text: "correction failed", Topic: "date", Risk: "medium", Negative: true}
		if _, e = s.Execute(ctx, c); e != nil {
			t.Fatal(e)
		}
	}
	c = cmd("feedback_create", "after-circuit")
	c.Payload = Payload{Text: "another recommendation", Topic: "date", Risk: "low"}
	later, e := s.Execute(ctx, c)
	if e != nil {
		t.Fatal(e)
	}
	*clock = clock.Add(25 * time.Hour)
	c = cmd("feedback_activate", "must-not-reactivate")
	c.ID = later.ID
	c.ExpectedRevision = later.Revision
	if _, e = s.Execute(ctx, c); e == nil {
		t.Fatal("activation bypassed topic circuit")
	}
	c = cmd("guidance_seed", "seed-after-circuit")
	c.Payload = Payload{Text: "manual seed after negative feedback", Topic: "date"}
	seed, e := s.Execute(ctx, c)
	if e != nil || seed.Status != "quarantined" {
		t.Fatal("new seed bypassed topic circuit", seed, e)
	}
	if e = db.View(ctx, func(tx *sql.Tx) error {
		var status string
		e := tx.QueryRow(`SELECT status FROM ext_feedback_guidances WHERE guidance_id=?`, f.ID).Scan(&status)
		if status != "quarantined" {
			t.Fatal("topic circuit not applied", status)
		}
		return e
	}); e != nil {
		t.Fatal(e)
	}
}
