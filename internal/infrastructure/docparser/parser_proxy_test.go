package docparser

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
)

const parserProxyTestKey = "native-proxy-fixture-key-not-secret"

func TestParserProxyDestinationAndSelection(t *testing.T) {
	t.Setenv("AGENTRAG_PARSER_PROXY_ADDR", "")
	if ParserProxyAddress("pdf", false) != "" {
		t.Fatal("unset proxy must preserve original reader")
	}
	t.Setenv("AGENTRAG_PARSER_PROXY_ADDR", "http://172.20.0.1:8090")
	if ParserProxyAddress("pdf", false) == "" {
		t.Fatal("configured host IP must be supported")
	}
	for _, typ := range []string{"docx", "mp3", "txt"} {
		if ParserProxyAddress(typ, false) != "" {
			t.Fatal("non PDF rerouted")
		}
	}
	if ParserProxyAddress("pdf", true) != "" {
		t.Fatal("URL rerouted")
	}
	for _, addr := range []string{"http://127.0.0.1:8090", "http://172.20.0.1:8090", "https://parser.example:8443"} {
		if _, err := parserProxyOrigin(addr); err != nil {
			t.Fatalf("explicit origin rejected: %v", err)
		}
	}
	for _, addr := range []string{"ftp://127.0.0.1", "http://u:p@127.0.0.1", "http://127.0.0.1/path", "http://127.0.0.1?x=1", "http://127.0.0.1#x"} {
		if _, err := parserProxyOrigin(addr); err == nil {
			t.Fatalf("bad origin accepted: %s", addr)
		}
	}
}

func TestParserProxyAllHTTPRequestsAndConcurrentIdentity(t *testing.T) {
	t.Setenv("AGENTRAG_PARSER_PROXY_KEY", parserProxyTestKey)
	f := &fakeMinerUV1{t: t, apiKey: parserProxyTestKey, zipData: v1TestZip(t), uploadURL: "/parser/native/v1/uploads/upload_1/content"}
	var mu sync.Mutex
	seen := map[string]map[string]bool{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		doc := r.Header.Get("X-AgentRAG-Doc-ID")
		if (doc != "doc-a" && doc != "doc-b") || r.Header.Get("X-AgentRAG-Tenant-ID") != "7" || r.Header.Get("X-AgentRAG-KB-ID") != "kb-a" || r.Header.Get("X-AgentRAG-Bridge-Version") != "1" {
			t.Error("identity absent or mixed")
		}
		mu.Lock()
		if seen[doc] == nil {
			seen[doc] = map[string]bool{}
		}
		seen[doc][r.Method+" "+r.URL.Path] = true
		mu.Unlock()
		clone := r.Clone(r.Context())
		clone.URL.Path = strings.TrimPrefix(r.URL.Path, "/parser/native")
		f.handler().ServeHTTP(w, clone)
	}))
	defer server.Close()
	var wg sync.WaitGroup
	for _, doc := range []string{"doc-a", "doc-b"} {
		wg.Add(1)
		go func(doc string) {
			defer wg.Done()
			reader := NewParserProxyReader(server.URL, ParserProxyIdentity{TenantID: 7, KBID: "kb-a", DocID: doc}, nil)
			result, err := reader.Read(context.Background(), &types.ReadRequest{FileContent: []byte("fixture"), FileName: "fixture.pdf", FileType: "pdf"})
			if err != nil || result == nil || result.MarkdownContent == "" {
				t.Errorf("native V1 result failed: %v", err)
			}
		}(doc)
	}
	wg.Wait()
	for _, doc := range []string{"doc-a", "doc-b"} {
		for _, leg := range []string{"POST /parser/native/v1/uploads", "PUT /parser/native/v1/uploads/upload_1/content", "POST /parser/native/v1/uploads/upload_1/complete", "POST /parser/native/v1/parse/jobs", "GET /parser/native/v1/parse/jobs/job_1"} {
			if !seen[doc][leg] {
				t.Errorf("missing identity on %s", leg)
			}
		}
	}
}

func TestParserProxyRejectsMissingIdentityCredentialAndRedirect(t *testing.T) {
	var reached atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached.Add(1)
		http.Redirect(w, r, "http://example.invalid", 307)
	}))
	defer server.Close()
	req := &types.ReadRequest{FileContent: []byte("fixture"), FileName: "x.pdf", FileType: "pdf"}
	t.Setenv("AGENTRAG_PARSER_PROXY_KEY", "")
	if _, err := NewParserProxyReader(server.URL, ParserProxyIdentity{TenantID: 7, KBID: "kb", DocID: "doc"}, nil).Read(context.Background(), req); err == nil {
		t.Fatal("missing credential allowed")
	}
	t.Setenv("AGENTRAG_PARSER_PROXY_KEY", parserProxyTestKey)
	if _, err := NewParserProxyReader(server.URL, ParserProxyIdentity{}, nil).Read(context.Background(), req); err == nil {
		t.Fatal("missing identity allowed")
	}
	if reached.Load() != 0 {
		t.Fatal("invalid request sent")
	}
	if _, err := NewParserProxyReader(server.URL, ParserProxyIdentity{TenantID: 7, KBID: "kb", DocID: "doc"}, nil).Read(context.Background(), req); err == nil {
		t.Fatal("redirect allowed")
	}
	if reached.Load() != 1 {
		t.Fatal("unexpected retry/fallback")
	}
}

func TestParserProxyDetachedCancellationKeepsIdentity(t *testing.T) {
	t.Setenv("AGENTRAG_PARSER_PROXY_KEY", parserProxyTestKey)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f := &fakeMinerUV1{t: t, apiKey: parserProxyTestKey, zipData: v1TestZip(t), uploadURL: "/parser/native/v1/uploads/upload_1/content", pollStatuses: []string{"running"}}
	var deleted atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "DELETE" {
			if r.Header.Get("X-AgentRAG-Doc-ID") != "doc-a" {
				t.Error("cancel identity lost")
			}
			deleted.Store(true)
		}
		clone := r.Clone(r.Context())
		clone.URL.Path = strings.TrimPrefix(r.URL.Path, "/parser/native")
		f.handler().ServeHTTP(w, clone)
		if r.Method == "GET" && strings.Contains(r.URL.Path, "/parse/jobs/") {
			cancel()
		}
	}))
	defer server.Close()
	bounded, stop := context.WithTimeout(ctx, 5*time.Second)
	defer stop()
	_, err := NewParserProxyReader(server.URL, ParserProxyIdentity{TenantID: 7, KBID: "kb", DocID: "doc-a"}, nil).Read(bounded, &types.ReadRequest{FileContent: []byte("fixture"), FileName: "x.pdf", FileType: "pdf"})
	if err == nil || !deleted.Load() {
		t.Fatal("cancel was not sent with independent identity")
	}
}

func TestParserProxyHTTPFailuresDoNotFallback(t *testing.T) {
	t.Setenv("AGENTRAG_PARSER_PROXY_KEY", parserProxyTestKey)
	id := ParserProxyIdentity{TenantID: 7, KBID: "kb", DocID: "doc"}
	req := &types.ReadRequest{FileContent: []byte("fixture"), FileName: "x.pdf", FileType: "pdf"}
	for _, status := range []int{401, 503} {
		var calls atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(status) }))
		if _, err := NewParserProxyReader(server.URL, id, nil).Read(context.Background(), req); err == nil {
			t.Errorf("HTTP %d allowed", status)
		}
		if calls.Load() != 1 {
			t.Errorf("HTTP %d retried/fell back", status)
		}
		server.Close()
	}
	server := httptest.NewServer(http.NotFoundHandler())
	addr := server.URL
	server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := NewParserProxyReader(addr, id, nil).Read(ctx, req); err == nil {
		t.Fatal("unavailable proxy allowed")
	}
}

func TestParserProxyUploadURLCannotEscapeOrigin(t *testing.T) {
	t.Setenv("AGENTRAG_PARSER_PROXY_KEY", parserProxyTestKey)
	var escaped atomic.Int32
	foreign := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { escaped.Add(1); w.WriteHeader(204) }))
	defer foreign.Close()
	f := &fakeMinerUV1{t: t, apiKey: parserProxyTestKey, zipData: v1TestZip(t), uploadURL: foreign.URL + "/parser/native/v1/uploads/u/content"}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clone := r.Clone(r.Context())
		clone.URL.Path = strings.TrimPrefix(r.URL.Path, "/parser/native")
		f.handler().ServeHTTP(w, clone)
	}))
	defer server.Close()
	_, err := NewParserProxyReader(server.URL, ParserProxyIdentity{TenantID: 7, KBID: "kb", DocID: "doc"}, nil).Read(context.Background(), &types.ReadRequest{FileContent: []byte("fixture"), FileName: "x.pdf", FileType: "pdf"})
	if err == nil || escaped.Load() != 0 {
		t.Fatal("foreign upload URL received credential/content")
	}
}
