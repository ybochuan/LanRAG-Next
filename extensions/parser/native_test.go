package parser

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const nativeTestKey = "native-host-fixture-key-not-secret"

func TestProxyBulkHasSeparateBudgetAndHonorsCancellation(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(100 * time.Millisecond):
			_, _ = w.Write([]byte("bulk"))
		case <-r.Context().Done():
		}
	}))
	defer backend.Close()
	p, err := NewScopeProxy(backend.URL, "http://127.0.0.1:8090", "", testBindings())
	if err != nil {
		t.Fatal(err)
	}
	p.client.Timeout = 10 * time.Millisecond
	p.bulkClient.Timeout = time.Second
	defer p.client.CloseIdleConnections()
	defer p.bulkClient.CloseIdleConnections()
	r := httptest.NewRequest("GET", "/", nil)
	if status, _, err := p.forward(r, "GET", "/v1/files/f/content", nil, true); err != nil || status != 200 {
		t.Fatalf("bulk inherited metadata deadline: %v", err)
	}
	if _, _, err := p.forward(r, "GET", "/v1/health", nil, false); err == nil {
		t.Fatal("metadata lost short deadline")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := p.forward(r.WithContext(ctx), "PUT", "/v1/uploads/u/content", []byte("file"), true); err == nil {
		t.Fatal("bulk ignored cancellation")
	}
}

func nativeCall(t *testing.T, h http.Handler, method, path, doc string, body any) proxyReply {
	t.Helper()
	var b []byte
	if raw, ok := body.([]byte); ok {
		b = raw
	} else if body != nil {
		b, _ = json.Marshal(body)
	}
	r := httptest.NewRequest(method, path, bytes.NewReader(b))
	r.Header.Set("Authorization", "Bearer "+nativeTestKey)
	r.Header.Set("X-AgentRAG-Bridge-Version", "1")
	r.Header.Set("X-AgentRAG-Tenant-ID", "7")
	r.Header.Set("X-AgentRAG-KB-ID", "kb-a")
	r.Header.Set("X-AgentRAG-Doc-ID", doc)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return proxyReply{w.Code, w.Body.Bytes()}
}

func nativeInput(t *testing.T, h http.Handler, doc string) string {
	t.Helper()
	content := testPDF()
	meta := sourceMetadata(content)
	r := nativeCall(t, h, "POST", "/parser/native/v1/uploads", doc, meta)
	if r.status != 200 {
		t.Fatalf("upload create HTTP %d", r.status)
	}
	m := r.object(t)
	u := m["upload_url"].(string)
	if !strings.HasPrefix(u, "/parser/native/v1/uploads/") {
		t.Fatal("upload URL must be relative to configured native origin")
	}
	if r := nativeCall(t, h, "PUT", u, doc, content); r.status != 204 {
		t.Fatalf("source HTTP %d", r.status)
	}
	r = nativeCall(t, h, "POST", "/parser/native/v1/uploads/"+m["id"].(string)+"/complete", doc, map[string]any{"sha256sum": meta["sha256sum"]})
	if r.status != 200 {
		t.Fatalf("complete HTTP %d", r.status)
	}
	return r.object(t)["file"].(map[string]any)["id"].(string)
}

func TestNativeIdentityAndSameBytesDifferentDocuments(t *testing.T) {
	f := fakeMinerUFixture(t)
	root := t.TempDir()
	h, err := NewNativeHandler(NativeConfig{UpstreamURL: f.server.URL, UpstreamKey: testBackendKey, APIKey: nativeTestKey, ArtifactRoot: root, Scopes: []ScopeIdentity{{TenantID: 7, KnowledgeBaseID: "kb-a"}}})
	if err != nil {
		t.Fatal(err)
	}
	firstJob := ""
	for _, doc := range []string{"doc-a", "doc-b"} {
		file := nativeInput(t, h, doc)
		if firstJob != "" {
			if r := nativeCall(t, h, "GET", "/parser/native/v1/parse/jobs/"+firstJob, doc, nil); r.status != 403 {
				t.Fatal("cross-document job readable")
			}
		}
		r := nativeCall(t, h, "POST", "/parser/native/v1/parse/jobs", doc, jobPayload(file))
		if r.status != 202 {
			t.Fatalf("job HTTP %d", r.status)
		}
		job := r.object(t)["job_id"].(string)
		if firstJob == "" {
			firstJob = job
		}
		nativeCall(t, h, "GET", "/parser/native/v1/parse/jobs/"+job, doc, nil)
		if r := nativeCall(t, h, "GET", "/parser/native/v1/files/z-"+job+"/content", doc, nil); r.status != 200 {
			t.Fatalf("ZIP HTTP %d", r.status)
		}
		if _, err := os.Stat(filepath.Join(root, doc, "middle.json")); err != nil {
			t.Fatal(err)
		}
	}
	if f.jobs.Load() != 2 || f.cached.Load() < 1 {
		t.Fatalf("jobs=%d cache=%d", f.jobs.Load(), f.cached.Load())
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, req := range f.jobRequests {
		if req.Tier != "flash" || req.OCRMode != "txt" {
			t.Fatal("OCR policy bypass")
		}
	}
}

func TestNativeRejectsCredentialsVersionAndHeaderForgery(t *testing.T) {
	f := fakeMinerUFixture(t)
	h, err := NewNativeHandler(NativeConfig{UpstreamURL: f.server.URL, UpstreamKey: testBackendKey, APIKey: nativeTestKey, ArtifactRoot: t.TempDir(), Scopes: []ScopeIdentity{{TenantID: 7, KnowledgeBaseID: "kb-a"}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, mutation := range []func(*http.Request){
		func(r *http.Request) { r.Header.Del("Authorization") }, func(r *http.Request) { r.Header.Set("Authorization", "Bearer wrong") },
		func(r *http.Request) { r.Header.Set("X-AgentRAG-Bridge-Version", "2") }, func(r *http.Request) { r.Header.Del("X-AgentRAG-Doc-ID") },
		func(r *http.Request) { r.Header.Set("X-AgentRAG-KB-ID", "other-kb") }, func(r *http.Request) { r.Header.Add("X-AgentRAG-Doc-ID", "other-doc") },
		func(r *http.Request) { r.Header.Set("X-AgentRAG-Doc-ID", "../escape") }, func(r *http.Request) { r.Header.Set("X-AgentRAG-Tenant-ID", "8") },
	} {
		r := httptest.NewRequest("GET", "/parser/native/v1/health", nil)
		r.Header.Set("Authorization", "Bearer "+nativeTestKey)
		r.Header.Set("X-AgentRAG-Bridge-Version", "1")
		r.Header.Set("X-AgentRAG-Tenant-ID", "7")
		r.Header.Set("X-AgentRAG-KB-ID", "kb-a")
		r.Header.Set("X-AgentRAG-Doc-ID", "doc-a")
		mutation(r)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code < 400 {
			t.Fatalf("forged identity accepted HTTP %d", w.Code)
		}
	}
	if f.calls.Load() != 0 {
		t.Fatal("rejected identity reached MinerU")
	}
}

func TestNativeRequiresPDFPreflightAndReportsOnlyArchivedOutput(t *testing.T) {
	f := fakeMinerUFixture(t)
	var events []string
	h, err := NewNativeHandler(NativeConfig{UpstreamURL: f.server.URL, UpstreamKey: testBackendKey, APIKey: nativeTestKey, ArtifactRoot: t.TempDir(), Scopes: []ScopeIdentity{{TenantID: 7, KnowledgeBaseID: "kb-a"}}, Report: func(code string) { events = append(events, code) }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.Close)
	content := []byte("not a PDF")
	meta := sourceMetadata(content)
	meta["filename"] = "source.txt"
	meta["mime_type"] = "text/plain"
	u := nativeCall(t, h, "POST", "/parser/native/v1/uploads", "doc-a", meta).object(t)
	if r := nativeCall(t, h, "PUT", u["upload_url"].(string), "doc-a", content); r.status != 422 {
		t.Errorf("native non-PDF bypassed required preflight: HTTP %d", r.status)
	}
	file := nativeInput(t, h, "doc-b")
	before := len(events)
	nativeCall(t, h, "GET", "/parser/native/v1/files/"+file+"/content", "doc-b", nil)
	for _, code := range events[before:] {
		if code == "parser_native_artifact_committed" {
			t.Error("source download falsely reported an archive commit")
		}
	}
}
