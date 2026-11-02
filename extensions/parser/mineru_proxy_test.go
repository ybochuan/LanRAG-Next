package parser

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const testBackendKey = "backend-key-not-a-real-secret"

func testBindings() []ScopeBinding {
	return []ScopeBinding{
		{RouteID: "a", TenantID: 7, KnowledgeBaseID: "kb-a", APIKey: "client-a-key-not-a-real-secret"},
		{RouteID: "b", TenantID: 7, KnowledgeBaseID: "kb-b", APIKey: "client-b-key-not-a-real-secret"},
	}
}

type proxyReply struct {
	status int
	body   []byte
}

func (r proxyReply) object(t *testing.T) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(r.body, &out); err != nil {
		t.Fatalf("non-JSON proxy response, HTTP %d", r.status)
	}
	return out
}

func proxyCall(t *testing.T, method, target, key string, value any, host string) proxyReply {
	t.Helper()
	var body []byte
	if raw, ok := value.([]byte); ok {
		body = raw
	} else if value != nil {
		var err error
		body, err = json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
	}
	r, err := http.NewRequest(method, target, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("Authorization", "Bearer "+key)
	r.Header.Set("Content-Type", "application/json")
	if host != "" {
		r.Host = host
	}
	c := &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := c.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		t.Fatal(err)
	}
	return proxyReply{resp.StatusCode, b}
}

func proxyFixture(t *testing.T, upstream, backendKey string, options ...ProxyOption) (*ScopeProxy, string) {
	t.Helper()
	s := httptest.NewUnstartedServer(nil)
	p, err := NewScopeProxy(upstream, "http://"+s.Listener.Addr().String(), backendKey, testBindings(), options...)
	if err != nil {
		s.Close()
		t.Fatal(err)
	}
	s.Config.Handler = p
	s.Start()
	t.Cleanup(s.Close)
	return p, s.URL
}

func scopeEndpoint(base, route string) string { return base + "/parser/scopes/" + route + "/v1" }
func sourceMetadata(content []byte) map[string]any {
	sum := sha256.Sum256(content)
	return map[string]any{"filename": "fixture.pdf", "bytes": len(content), "mime_type": "application/pdf", "purpose": "parse", "sha256sum": hex.EncodeToString(sum[:])}
}
func uploadThroughProxy(t *testing.T, endpoint, key string, content []byte) string {
	t.Helper()
	meta := sourceMetadata(content)
	created := proxyCall(t, "POST", endpoint+"/uploads", key, meta, "attacker.invalid").object(t)
	if created["status"] != "pending" {
		t.Fatal("proxy must require bytes even on backend cache hit")
	}
	uploadURL, _ := created["upload_url"].(string)
	if !strings.HasPrefix(uploadURL, endpoint+"/uploads/") {
		t.Fatal("upload escaped configured scope origin")
	}
	r := proxyCall(t, "PUT", uploadURL, key, content, "")
	if r.status != 200 && r.status != 204 {
		t.Fatalf("upload HTTP %d", r.status)
	}
	id, _ := created["id"].(string)
	r = proxyCall(t, "POST", endpoint+"/uploads/"+id+"/complete", key, map[string]any{"sha256sum": meta["sha256sum"]}, "")
	if r.status != 200 {
		t.Fatalf("complete HTTP %d", r.status)
	}
	return r.object(t)["file"].(map[string]any)["id"].(string)
}
func jobPayload(fileID string) map[string]any {
	return map[string]any{"files": []any{map[string]any{"source": map[string]any{"type": "file_id", "file_id": fileID}}}, "tier": "flash", "ocr_mode": "auto", "output_formats": []string{"zip"}}
}

type fakeMinerU struct {
	server         *httptest.Server
	jobs           atomic.Int32
	calls          atomic.Int32
	cached         atomic.Int32
	failJob        atomic.Bool
	varyFileIDs    atomic.Bool
	cancelPoll     atomic.Bool
	polls          atomic.Int32
	cancels        atomic.Int32
	privateFailure atomic.Bool
	mu             sync.Mutex
	uploads        map[string]map[string]any
	files          map[string][]byte
	jobRequests    []jobRequest
	jobGate        chan struct{}
}

func fakeMinerUFixture(t *testing.T) *fakeMinerU {
	t.Helper()
	f := &fakeMinerU{uploads: map[string]map[string]any{}, files: map[string][]byte{}}
	f.server = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.calls.Add(1)
		if r.Header.Get("Authorization") != "Bearer "+testBackendKey {
			w.WriteHeader(401)
			return
		}
		jsonOut := func(status int, v any) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_ = json.NewEncoder(w).Encode(v)
		}
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		switch {
		case r.URL.Path == "/v1/health":
			jsonOut(200, map[string]any{"status": "ok"})
		case r.URL.Path == "/v1/uploads" && r.Method == "POST":
			var m map[string]any
			_ = json.NewDecoder(r.Body).Decode(&m)
			hash, _ := m["sha256sum"].(string)
			fileID := "f-" + hash
			f.mu.Lock()
			_, cached := f.files[fileID]
			cached = cached && !f.varyFileIDs.Load()
			id := fmt.Sprintf("u-%d", len(f.uploads)+1)
			f.uploads[id] = m
			f.mu.Unlock()
			if cached {
				f.cached.Add(1)
				jsonOut(200, map[string]any{"id": id, "status": "completed", "file": map[string]any{"id": fileID}})
			} else {
				jsonOut(200, map[string]any{"id": id, "status": "pending", "upload_url": f.server.URL + "/v1/uploads/" + id + "/content", "upload_method": "PUT"})
			}
		case len(parts) == 4 && parts[1] == "uploads" && parts[3] == "content":
			b, _ := io.ReadAll(r.Body)
			f.mu.Lock()
			m := f.uploads[parts[2]]
			m["content"] = b
			f.mu.Unlock()
			w.WriteHeader(204)
		case len(parts) == 4 && parts[1] == "uploads" && parts[3] == "complete":
			f.mu.Lock()
			m := f.uploads[parts[2]]
			fileID := "f-" + m["sha256sum"].(string)
			if f.varyFileIDs.Load() {
				fileID += "-" + parts[2]
			}
			f.files[fileID] = m["content"].([]byte)
			f.mu.Unlock()
			jsonOut(200, map[string]any{"status": "completed", "file": map[string]any{"id": fileID}})
		case r.URL.Path == "/v1/parse/jobs" && r.Method == "POST":
			var input jobRequest
			if json.NewDecoder(r.Body).Decode(&input) != nil {
				w.WriteHeader(400)
				return
			}
			f.mu.Lock()
			f.jobRequests = append(f.jobRequests, input)
			f.mu.Unlock()
			n := f.jobs.Add(1)
			if f.jobGate != nil {
				<-f.jobGate
			}
			if f.failJob.Load() {
				jsonOut(503, map[string]any{"error": "upstream-private-detail"})
				return
			}
			if f.privateFailure.Load() {
				jsonOut(202, privateFailedJob(fmt.Sprintf("j-%d", n)))
				return
			}
			jsonOut(202, map[string]any{"job_id": fmt.Sprintf("j-%d", n), "status": "pending", "files": []any{}})
		case len(parts) == 4 && parts[1] == "parse" && parts[2] == "jobs":
			if r.Method == "GET" && f.cancelPoll.Load() {
				f.polls.Add(1)
				<-r.Context().Done()
				return
			}
			if r.Method == "DELETE" {
				f.cancels.Add(1)
			}
			if f.privateFailure.Load() {
				jsonOut(200, privateFailedJob(parts[3]))
				return
			}
			jsonOut(200, map[string]any{"job_id": parts[3], "status": "completed", "tier": "flash", "files": []any{map[string]any{"name": "fixture.pdf", "status": "completed", "output_files": map[string]any{"zip": map[string]any{"file_id": "z-" + parts[3], "bytes": 128}}, "parse": map[string]any{"parser_version": "4.0.1"}}}})
		case len(parts) == 4 && parts[1] == "files" && parts[3] == "content":
			_, _ = w.Write(middleZIP(t, "middle_json.json"))
		default:
			w.WriteHeader(404)
		}
	}))
	f.server.Start()
	t.Cleanup(f.server.Close)
	return f
}

func privateFailedJob(id string) map[string]any {
	return map[string]any{"job_id": id, "status": "failed", "files": []any{map[string]any{"name": "fixture.pdf", "status": "failed", "error": map[string]any{"code": "parse_failed", "message": "private-backend-exception /private/config key=not-public", "param": "private-backend-parameter"}}}}
}

func TestScopeProxyMasksAsyncFailureOnCreatePollAndCancel(t *testing.T) {
	for _, immediate := range []bool{false, true} {
		t.Run(fmt.Sprint(immediate), func(t *testing.T) {
			f := fakeMinerUFixture(t)
			_, base := proxyFixture(t, f.server.URL, testBackendKey)
			ep := scopeEndpoint(base, "a")
			key := testBindings()[0].APIKey
			file := uploadThroughProxy(t, ep, key, testPDF())
			f.privateFailure.Store(immediate)
			created := proxyCall(t, "POST", ep+"/parse/jobs", key, jobPayload(file), "")
			job := created.object(t)["job_id"].(string)
			f.privateFailure.Store(true)
			replies := []proxyReply{proxyCall(t, "GET", ep+"/parse/jobs/"+job, key, nil, ""), proxyCall(t, "DELETE", ep+"/parse/jobs/"+job, key, nil, "")}
			if immediate {
				replies = append(replies, created, proxyCall(t, "POST", ep+"/parse/jobs", key, jobPayload(file), ""))
			}
			for _, reply := range replies {
				if strings.Contains(string(reply.body), "private-backend") {
					t.Fatal("HTTP success carried a private asynchronous exception")
				}
				if reply.object(t)["status"] != "failed" {
					t.Fatal("masking changed the terminal failure state")
				}
			}
		})
	}
}

func TestScopeProxyColdAndCachedSourcesRemainScoped(t *testing.T) {
	f := fakeMinerUFixture(t)
	_, base := proxyFixture(t, f.server.URL, testBackendKey)
	bindings := testBindings()
	content := testPDF()
	a, b := scopeEndpoint(base, "a"), scopeEndpoint(base, "b")
	fileA := uploadThroughProxy(t, a, bindings[0].APIKey, content)
	if r := proxyCall(t, "POST", b+"/parse/jobs", bindings[1].APIKey, jobPayload(fileA), ""); r.status != 403 {
		t.Fatalf("cross-scope input HTTP %d", r.status)
	}
	if f.jobs.Load() != 0 {
		t.Fatal("cross-scope request created job")
	}
	fileB := uploadThroughProxy(t, b, bindings[1].APIKey, content)
	if f.cached.Load() != 1 || fileA != fileB {
		t.Fatal("fixture did not exercise shared backend source cache")
	}
	r := proxyCall(t, "POST", a+"/parse/jobs", bindings[0].APIKey, jobPayload(fileA), "")
	if r.status != 202 {
		t.Fatalf("create HTTP %d", r.status)
	}
	jobID := r.object(t)["job_id"].(string)
	if r := proxyCall(t, "GET", b+"/parse/jobs/"+jobID, bindings[1].APIKey, nil, ""); r.status != 403 {
		t.Fatalf("cross-scope job HTTP %d", r.status)
	}
	r = proxyCall(t, "GET", a+"/parse/jobs/"+jobID, bindings[0].APIKey, nil, "")
	if r.status != 200 {
		t.Fatal("own job denied")
	}
	zipID := "z-" + jobID
	if r := proxyCall(t, "GET", b+"/files/"+zipID+"/content", bindings[1].APIKey, nil, ""); r.status != 403 {
		t.Fatalf("cross-scope artifact HTTP %d", r.status)
	}
	if r := proxyCall(t, "GET", a+"/files/"+zipID+"/content", bindings[0].APIKey, nil, ""); r.status != 200 || len(r.body) == 0 {
		t.Fatal("own artifact unavailable")
	}
	if r := proxyCall(t, "POST", a+"/parse/jobs", bindings[0].APIKey, jobPayload(zipID), ""); r.status != 403 {
		t.Fatal("artifact bypassed source-byte verification")
	}
	if r := proxyCall(t, "POST", b+"/parse/jobs", bindings[1].APIKey, jobPayload(fileB), ""); r.status != 202 {
		t.Fatal("byte-proven second scope denied")
	}
}

func TestScopeProxyRejectsBadCredentialsAndUnverifiedBytes(t *testing.T) {
	f := fakeMinerUFixture(t)
	_, base := proxyFixture(t, f.server.URL, testBackendKey)
	key := testBindings()[0].APIKey
	a := scopeEndpoint(base, "a")
	for _, target := range []string{a, scopeEndpoint(base, "missing")} {
		if r := proxyCall(t, "GET", target+"/health", "wrong-key", nil, ""); r.status != 403 {
			t.Fatal("unknown scope or credential permitted")
		}
	}
	if f.calls.Load() != 0 {
		t.Fatal("unauthorized request reached backend")
	}
	meta := sourceMetadata([]byte("correct"))
	r := proxyCall(t, "POST", a+"/uploads", key, meta, "").object(t)
	id := r["id"].(string)
	if r := proxyCall(t, "POST", a+"/uploads/"+id+"/complete", key, map[string]any{"sha256sum": meta["sha256sum"]}, ""); r.status != 409 {
		t.Fatal("complete before bytes permitted")
	}
	if r := proxyCall(t, "PUT", r["upload_url"].(string), key, []byte("wrong!!"), ""); r.status != 400 {
		t.Fatal("bad source hash permitted")
	}
	if f.jobs.Load() != 0 {
		t.Fatal("bad source created job")
	}
	if r := proxyCall(t, "POST", a+"/parse/jobs", key, map[string]any{"files": []any{map[string]any{"source": map[string]any{"type": "url", "url": "http://invalid/"}}}, "output_formats": []string{"zip"}}, ""); r.status != 400 {
		t.Fatal("URL source permitted")
	}
}

func TestScopeProxyDeduplicatesConcurrentJobsAndBlocksUnknownOutcome(t *testing.T) {
	for _, unknown := range []bool{false, true} {
		t.Run(fmt.Sprint(unknown), func(t *testing.T) {
			f := fakeMinerUFixture(t)
			f.failJob.Store(unknown)
			f.jobGate = make(chan struct{})
			defer close(f.jobGate)
			_, base := proxyFixture(t, f.server.URL, testBackendKey)
			a := scopeEndpoint(base, "a")
			key := testBindings()[0].APIKey
			file := uploadThroughProxy(t, a, key, testPDF())
			payload := jobPayload(file)
			first := make(chan proxyReply, 1)
			go func() { defer close(first); first <- proxyCall(t, "POST", a+"/parse/jobs", key, payload, "") }()
			deadline := time.Now().Add(time.Second)
			for f.jobs.Load() != 1 && time.Now().Before(deadline) {
				time.Sleep(time.Millisecond)
			}
			if f.jobs.Load() != 1 {
				t.Fatal("first job not submitted")
			}
			if r := proxyCall(t, "POST", a+"/parse/jobs", key, payload, ""); r.status != 409 {
				t.Fatalf("concurrent duplicate HTTP %d", r.status)
			}
			f.jobGate <- struct{}{}
			initial, ok := <-first
			if !ok {
				t.Fatal("first request failed before producing a reply")
			}
			r := proxyCall(t, "POST", a+"/parse/jobs", key, payload, "")
			if unknown {
				if initial.status != 503 || r.status != 503 || strings.Contains(string(r.body), "upstream-private-detail") {
					t.Fatal("unknown result retried or leaked")
				}
			} else if r.status != 202 || !bytes.Equal(r.body, initial.body) {
				t.Fatal("known receipt not reused")
			}
			if f.jobs.Load() != 1 {
				t.Fatal("duplicate upstream parse")
			}
		})
	}
}

func TestScopeProxyValidatesConfigurationAndHidesCredentials(t *testing.T) {
	bindings := testBindings()
	for _, origin := range []string{"http://0.0.0.0:8000", "http://localhost:8000", "http://127.0.0.1:8000/?x=1", "http://user:secret@127.0.0.1:8000"} {
		if _, err := NewScopeProxy(origin, "http://127.0.0.1:8081", "", bindings); err == nil {
			t.Fatal("unsafe upstream accepted")
		}
	}
	for _, invalid := range [][]ScopeBinding{nil, {{RouteID: "a", TenantID: 0, KnowledgeBaseID: "kb", APIKey: "long-enough-test-key"}}, {bindings[0], bindings[0]}} {
		if _, err := NewScopeProxy("http://127.0.0.1:8000", "http://127.0.0.1:8081", "", invalid); err == nil {
			t.Fatal("invalid binding accepted")
		}
	}
	if strings.Contains(fmt.Sprintf("%+v %#v", bindings[0], bindings[0]), bindings[0].APIKey) {
		t.Fatal("binding formatting leaks credential")
	}
}

func TestScopeProxyResolvesOnlyAuthenticatedConfiguredIdentity(t *testing.T) {
	f := fakeMinerUFixture(t)
	p, _ := proxyFixture(t, f.server.URL, testBackendKey)
	identity, err := p.ResolveScope("a", testBindings()[0].APIKey)
	if err != nil || identity != (ScopeIdentity{TenantID: 7, KnowledgeBaseID: "kb-a"}) {
		t.Fatal("authenticated scope did not retain its configured identity")
	}
	identity, err = p.ResolveScope("a", testBindings()[1].APIKey)
	if err == nil || identity != (ScopeIdentity{}) {
		t.Fatal("wrong scope key disclosed an identity")
	}
	if strings.Contains(fmt.Sprintf("%+v %#v", p, p), testBackendKey) {
		t.Fatal("proxy formatting leaked backend credential")
	}
}

func TestScopeProxyRejectsLimitsAndNeverFollowsBackendRedirects(t *testing.T) {
	var leaked atomic.Int32
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { leaked.Add(1); w.WriteHeader(200) }))
	defer destination.Close()
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, destination.URL, 307) }))
	defer backend.Close()
	_, base := proxyFixture(t, backend.URL, testBackendKey)
	endpoint := scopeEndpoint(base, "a")
	key := testBindings()[0].APIKey
	if r := proxyCall(t, "GET", endpoint+"/health", key, nil, ""); r.status != 503 || leaked.Load() != 0 {
		t.Fatal("backend redirect followed")
	}
	if r := proxyCall(t, "POST", endpoint+"/uploads", key, bytes.Repeat([]byte(" "), (64<<10)+1), ""); r.status != 413 {
		t.Fatal("oversize JSON accepted")
	}
	meta := sourceMetadata([]byte("source"))
	meta["bytes"] = (50 << 20) + 1
	if r := proxyCall(t, "POST", endpoint+"/uploads", key, meta, ""); r.status != 400 {
		t.Fatal("oversize source declaration accepted")
	}
}

func TestScopeProxyRealMinerU401(t *testing.T) {
	upstream := os.Getenv("AGENTRAG_M1_POC_UPSTREAM")
	if upstream == "" {
		t.Skip("run extensions/scripts/m1_scope_poc.py for real MinerU4.0.1")
	}
	key := os.Getenv("AGENTRAG_M1_POC_BACKEND_KEY")
	artifactRoot := filepath.Join(t.TempDir(), "data", "parser_artifacts")
	p, base := proxyFixture(t, upstream, key, WithArtifactRoot(artifactRoot))
	var jobs, cached, unsafe atomic.Int32
	p.client.Transport = &countingTransport{base: p.client.Transport, jobs: &jobs, cached: &cached, unsafe: &unsafe}
	content := testPDF()
	bindings := testBindings()
	var firstJob, firstZip string
	for _, binding := range bindings {
		ep := scopeEndpoint(base, binding.RouteID)
		if firstJob != "" {
			if r := proxyCall(t, "GET", ep+"/parse/jobs/"+firstJob, binding.APIKey, nil, ""); r.status != 403 {
				t.Fatal("real cross-scope job permitted")
			}
			if r := proxyCall(t, "GET", ep+"/files/"+firstZip+"/content", binding.APIKey, nil, ""); r.status != 403 {
				t.Fatal("real cross-scope artifact permitted")
			}
		}
		if r := proxyCall(t, "GET", ep+"/health", binding.APIKey, nil, ""); r.status != 200 {
			t.Fatalf("health HTTP %d", r.status)
		}
		file := uploadThroughProxy(t, ep, binding.APIKey, content)
		if err := p.BindDocument(binding.RouteID, file, "fixture-doc-"+binding.RouteID); err != nil {
			t.Fatal(err)
		}
		s := p.scopes[binding.RouteID]
		s.mu.Lock()
		report := s.pdfs[file]
		s.mu.Unlock()
		if report.TotalPages != 1 || report.TextLayerPages != 1 || report.ImageOnlyPages != 0 {
			t.Fatalf("real preflight=%+v", report)
		}
		payload := jobPayload(file)
		payload["tier"] = "advanced"
		if binding.RouteID == "b" {
			payload["ocr_mode"] = "ocr"
		} // Scope a retains caller auto.
		r := proxyCall(t, "POST", ep+"/parse/jobs", binding.APIKey, payload, "")
		if r.status != 202 && r.status != 200 {
			t.Fatalf("real job HTTP %d", r.status)
		}
		job := r.object(t)["job_id"].(string)
		deadline := time.Now().Add(45 * time.Second)
		var result map[string]any
		for time.Now().Before(deadline) {
			result = proxyCall(t, "GET", ep+"/parse/jobs/"+job, binding.APIKey, nil, "").object(t)
			if result["status"] == "completed" {
				break
			}
			if result["status"] == "failed" {
				t.Fatal("real Flash parse failed")
			}
			time.Sleep(100 * time.Millisecond)
		}
		if result["status"] != "completed" {
			t.Fatal("real parse deadline")
		}
		f := result["files"].([]any)[0].(map[string]any)
		if f["parse"].(map[string]any)["parser_version"] != "4.0.1" {
			t.Fatal("backend parser version drift")
		}
		zipID := f["output_files"].(map[string]any)["zip"].(map[string]any)["file_id"].(string)
		if firstJob == "" {
			firstJob, firstZip = job, zipID
		}
		z := proxyCall(t, "GET", ep+"/files/"+zipID+"/content", binding.APIKey, nil, "")
		if z.status != 200 {
			t.Fatal("real ZIP unavailable")
		}
		archive, err := zip.NewReader(bytes.NewReader(z.body), int64(len(z.body)))
		if err != nil || len(archive.File) == 0 {
			t.Fatal("invalid real archive")
		}
		middle, err := os.ReadFile(filepath.Join(artifactRoot, "fixture-doc-"+binding.RouteID, "middle.json"))
		var header struct {
			Schema string            `json:"schema_version"`
			Pages  []json.RawMessage `json:"pages"`
		}
		if err != nil || json.Unmarshal(middle, &header) != nil || header.Schema != "2.0" || len(header.Pages) != 1 {
			t.Fatal("real document-bound middle JSON missing/invalid")
		}
		t.Log("preflight pages=1 text=1; caller auto/ocr+advanced reached backend as flash/txt; middle.json saved under explicit fixture doc_id, schema=2.0")
	}
	if jobs.Load() != 2 {
		t.Fatalf("real upstream jobs=%d, want one per explicit scope", jobs.Load())
	}
	if cached.Load() < 1 {
		t.Fatal("real backend source cache was not exercised")
	}
	if unsafe.Load() != 0 {
		t.Fatal("unsafe OCR request attempted")
	}
	t.Log("real backend cache hit verified; two byte-proven scopes, two total formal jobs")
}

type countingTransport struct {
	base   http.RoundTripper
	jobs   *atomic.Int32
	cached *atomic.Int32
	unsafe *atomic.Int32
}

func (c *countingTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.Method == "POST" && r.URL.Path == "/v1/parse/jobs" {
		body, err := io.ReadAll(io.LimitReader(r.Body, proxyJSONLimit+1))
		r.Body.Close()
		r.Body = io.NopCloser(bytes.NewReader(body))
		var input jobRequest
		if err != nil || json.Unmarshal(body, &input) != nil || input.Tier != "flash" || input.OCRMode != "txt" {
			c.unsafe.Add(1)
			return nil, errors.New("real POC refused unsafe OCR policy before backend side effects")
		}
		c.jobs.Add(1)
	}
	resp, err := c.base.RoundTrip(r)
	if err == nil && r.Method == "POST" && r.URL.Path == "/v1/uploads" {
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		resp.Body.Close()
		resp.Body = io.NopCloser(bytes.NewReader(body))
		if readErr != nil {
			return resp, readErr
		}
		var value map[string]any
		if json.Unmarshal(body, &value) == nil && value["status"] == "completed" {
			c.cached.Add(1)
		}
	}
	return resp, err
}
func testPDF() []byte {
	objects := []string{"<< /Type /Catalog /Pages 2 0 R >>", "<< /Type /Pages /Kids [3 0 R] /Count 1 >>", "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>", "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>"}
	text := "BT /F1 12 Tf 72 720 Td (AGENTRAG PRIVATE SCOPE FIXTURE) Tj ET\n"
	objects = append(objects, fmt.Sprintf("<< /Length %d >>\nstream\n%sendstream", len(text), text))
	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n")
	offsets := []int{0}
	for i, o := range objects {
		offsets = append(offsets, b.Len())
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(offsets))
	for _, offset := range offsets[1:] {
		fmt.Fprintf(&b, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(offsets), xref)
	return b.Bytes()
}
