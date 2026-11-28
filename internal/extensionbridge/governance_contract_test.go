package extensionbridge

import (
	"context"
	"encoding/json"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// contractRoute sends the production fixed loopback URL to a test-owned server.
type contractRoute struct {
	t           *testing.T
	path        string
	destination *url.URL
	transport   http.RoundTripper
}

func (rt contractRoute) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.String() != "http://127.0.0.1:8090"+rt.path {
		rt.t.Errorf("unexpected bridge destination: %s", r.URL)
	}
	copy := r.Clone(r.Context())
	u := *r.URL
	u.Scheme, u.Host = rt.destination.Scheme, rt.destination.Host
	copy.URL = &u
	return rt.transport.RoundTrip(copy)
}

func routeContractTo(t *testing.T, path, destination string) {
	t.Helper()
	u, err := url.Parse(destination)
	if err != nil {
		t.Fatal(err)
	}
	transport := &http.Transport{Proxy: nil}
	original := bridgeHTTPClient
	copy := *original
	copy.Transport = contractRoute{t, path, u, transport}
	bridgeHTTPClient = &copy
	t.Cleanup(func() { bridgeHTTPClient = original; transport.CloseIdleConnections() })
}

func TestGovernanceContractAllowedDocumentsConsumesBridgeDecision(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != http.MethodPost || r.URL.Path != "/bridge/before-retrieve" || r.Header.Get("X-AgentRAG-Bridge-Version") != "1" {
			t.Errorf("wrong request: %s %s %v", r.Method, r.URL.Path, r.Header)
		}
		var payload struct {
			RequestID        string   `json:"request_id"`
			SessionID        string   `json:"session_id"`
			Mode             string   `json:"mode"`
			TenantID         uint64   `json:"tenant_id"`
			KnowledgeBaseIDs []string `json:"knowledge_base_ids"`
			KnowledgeIDs     []string `json:"knowledge_ids"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil || payload.TenantID != 7 || len(payload.KnowledgeBaseIDs) != 1 || payload.KnowledgeBaseIDs[0] != "kb-1" || len(payload.KnowledgeIDs) != 2 || payload.Mode != "agent" || payload.SessionID == "" {
			t.Errorf("payload lost: %+v %v", payload, err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"request_id":"` + payload.RequestID + `","tenant_id":7,"knowledge_base_ids":["kb-1"],"knowledge_ids":["doc-1"],"excluded_document_ids":["doc-2"],"deny_all":false}`))
	}))
	t.Cleanup(server.Close)
	routeContractTo(t, "/bridge/before-retrieve", server.URL)
	allowed, err := AllowedDocuments(context.Background(), 7, "kb-1", []string{"doc-1", "doc-2"})
	if err != nil || calls.Load() != 1 || len(allowed) != 1 || !allowed["doc-1"] || allowed["doc-2"] {
		t.Fatalf("decision not consumed: allowed=%v err=%v calls=%d", allowed, err, calls.Load())
	}
}

func TestGovernanceContractAllowedDocumentsInScopePreservesEveryKB(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			RequestID        string   `json:"request_id"`
			KnowledgeBaseIDs []string `json:"knowledge_base_ids"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil || len(payload.KnowledgeBaseIDs) != 2 || payload.KnowledgeBaseIDs[0] != "kb-1" || payload.KnowledgeBaseIDs[1] != "kb-2" {
			t.Errorf("scope lost: %+v %v", payload, err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"request_id":"` + payload.RequestID + `","tenant_id":7,"knowledge_base_ids":["kb-1","kb-2"],"knowledge_ids":["doc-1"],"excluded_document_ids":[],"deny_all":false}`))
	}))
	t.Cleanup(server.Close)
	routeContractTo(t, "/bridge/before-retrieve", server.URL)
	allowed, err := AllowedDocumentsInScope(context.Background(), 7, []string{"kb-1", "kb-2"}, []string{"doc-1"})
	if err != nil || len(allowed) != 1 || !allowed["doc-1"] {
		t.Fatalf("multi-KB decision not consumed: %v %v", allowed, err)
	}
}

func TestGovernanceContractCorrectionSourcesRejectsForeignDocument(t *testing.T) {
	validHash := strings.Repeat("a", 64)
	for _, test := range []struct {
		name      string
		sources   string
		wantCount int
	}{
		{name: "allowed", sources: `[{"document_id":"doc-1","chunk_id":"chunk-1","content_hash":"` + validHash + `"}]`, wantCount: 1},
		{name: "foreign_document", sources: `[{"document_id":"doc-2","chunk_id":"chunk-1","content_hash":"` + validHash + `"}]`, wantCount: 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("AGENTRAG_CORRECTION_SOURCES_KEY", strings.Repeat("k", 32))
			t.Setenv("AGENTRAG_BEFORE_GENERATE_KEY", "")
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.URL.Path != "/bridge/correction-sources" || !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") || r.Header.Get("X-AgentRAG-Bridge-Version") != "1" {
					t.Errorf("wrong correction request: %s %s %v", r.Method, r.URL.Path, r.Header)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"sources":` + test.sources + `}`))
			}))
			t.Cleanup(server.Close)
			routeContractTo(t, "/bridge/correction-sources", server.URL)
			got := CorrectionSources(context.Background(), 7, "kb-1", []string{"doc-1"}, "query", "question")
			if len(got) != test.wantCount {
				t.Fatalf("sources=%v want=%d", got, test.wantCount)
			}
		})
	}
}

func TestGovernanceContractRejectUnattributedFailsClosed(t *testing.T) {
	if err := RejectUnattributed(context.Background()); !errors.Is(err, ErrUnattributed) {
		t.Fatalf("unattributed content was not rejected: %v", err)
	}
}

func TestGovernanceHookCallSitesRemainWired(t *testing.T) {
	root := filepath.Join("..", "..")
	cases := []struct {
		file   string
		callee string
	}{
		{"internal/application/service/chat_pipeline/search_parallel.go", "extensionbridge.BeforeRetrieve"},
		{"internal/application/service/chat_pipeline/governance.go", "governedDocuments"},
		{"internal/application/service/chat_pipeline/query_understand.go", "extensionbridge.RejectUnattributed"},
		{"internal/agent/resume.go", "extensionbridge.RejectUnattributed"},
		{"internal/agent/tools/governance.go", "governedDocuments"},
		{"internal/agent/tools/correction_sources.go", "extensionbridge.CorrectionSources"},
		{"internal/application/service/chat_pipeline/chat_completion.go", "extensionbridge.BeginGuidedSourceCall"},
		{"internal/application/service/chat_pipeline/chat_completion_stream.go", "extensionbridge.BeginGuidedSourceCall"},
		{"internal/agent/think.go", "extensionbridge.BeginGuidedSourceCall"},
		{"internal/application/service/chat_pipeline/into_chat_message.go", "extensionbridge.EnrichAttachmentResults"},
		{"internal/agent/tools/search_knowledge.go", "extensionbridge.EnrichAttachmentResults"},
	}
	for _, tc := range cases {
		t.Run(tc.callee+"_"+filepath.Base(tc.file), func(t *testing.T) {
			parsed, err := parser.ParseFile(token.NewFileSet(), filepath.Join(root, filepath.FromSlash(tc.file)), nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			ast.Inspect(parsed, func(node ast.Node) bool {
				switch value := node.(type) {
				case *ast.CallExpr:
					if callee, ok := value.Fun.(*ast.Ident); ok && callee.Name == tc.callee {
						found = true
					}
				case *ast.SelectorExpr:
					if pkg, ok := value.X.(*ast.Ident); ok && pkg.Name+"."+value.Sel.Name == tc.callee {
						found = true
					}
				}
				return !found
			})
			if !found {
				t.Fatalf("hook call %q is missing from %s", tc.callee, tc.file)
			}
		})
	}
}
