package parser

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/store"
)

// NativeConfig is operator configuration; request headers cannot set destinations.
type NativeConfig struct {
	UpstreamURL  string          `json:"upstream_url"`  // Explicit MinerU V1 origin.
	UpstreamKey  string          `json:"-"`             // Backend credential, never the caller credential.
	APIKey       string          `json:"-"`             // Shared trusted-core credential.
	ArtifactRoot string          `json:"artifact_root"` // Required independent artifact directory.
	Scopes       []ScopeIdentity `json:"scopes"`        // Authorized native owner tenant/KB pairs.
	Report       func(string)    `json:"-"`             // Fixed non-sensitive event codes only.
	BulkTimeout  time.Duration   `json:"bulk_timeout"`  // Zero uses 1000s, maximum 24h.
}

func (NativeConfig) String() string     { return "native parser configuration (redacted)" }
func (c NativeConfig) GoString() string { return c.String() }

// NativeHandler adapts identity-bearing native V1 requests to isolated doc scopes.
// Live scopes are bounded; production uses a durable journal to restore idle or
// restarted scopes. Unknown remote outcomes are never blindly retried.
type NativeHandler struct {
	cfg       NativeConfig
	auth      [32]byte
	mu        sync.Mutex
	documents map[nativeIdentity]*ScopeProxy
	allowed   map[ScopeIdentity]bool
	db        *store.DB // Nil only for the explicitly process-local compatibility constructor.
}
type nativeIdentity struct {
	ScopeIdentity
	DocID string
}

func (*NativeHandler) String() string { return "native parser handler (redacted)" }

// NewNativeHandler validates configuration without network calls or file writes.
func NewNativeHandler(cfg NativeConfig) (*NativeHandler, error) {
	if cfg.BulkTimeout == 0 {
		cfg.BulkTimeout = 1000 * time.Second
	}
	if cfg.BulkTimeout < time.Millisecond || cfg.BulkTimeout > 24*time.Hour {
		return nil, errors.New("invalid parser bulk timeout")
	}
	u, err := url.Parse(cfg.UpstreamURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || (u.Path != "" && u.Path != "/") || u.RawPath != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.TrimSpace(cfg.ArtifactRoot) == "" || len(cfg.APIKey) < 16 || len(cfg.APIKey) > 256 || strings.IndexFunc(cfg.APIKey, func(r rune) bool { return r <= 32 || r >= 127 }) >= 0 || len(cfg.Scopes) == 0 || len(cfg.Scopes) > 1000 {
		return nil, errors.New("invalid native parser configuration")
	}
	h := &NativeHandler{cfg: cfg, auth: sha256.Sum256([]byte("Bearer " + cfg.APIKey)), documents: map[nativeIdentity]*ScopeProxy{}, allowed: map[ScopeIdentity]bool{}}
	for _, scope := range cfg.Scopes {
		if scope.TenantID == 0 || !safeID(scope.KnowledgeBaseID) || h.allowed[scope] {
			return nil, errors.New("invalid native parser scope")
		}
		h.allowed[scope] = true
	}
	return h, nil
}

func (h *NativeHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !strings.HasPrefix(r.URL.Path, "/parser/native/v1/") || r.URL.RawPath != "" || r.URL.RawQuery != "" || r.URL.ForceQuery {
		proxyError(w, 404, "unknown_route")
		return
	}
	for _, key := range []string{"Authorization", "X-AgentRAG-Bridge-Version", "X-AgentRAG-Tenant-ID", "X-AgentRAG-KB-ID", "X-AgentRAG-Doc-ID"} {
		if len(r.Header.Values(key)) != 1 {
			proxyError(w, 403, "native_identity_denied")
			return
		}
	}
	auth := sha256.Sum256([]byte(r.Header.Get("Authorization")))
	if subtle.ConstantTimeCompare(auth[:], h.auth[:]) != 1 {
		proxyError(w, 403, "native_identity_denied")
		return
	}
	if r.Header.Get("X-AgentRAG-Bridge-Version") != "1" {
		proxyError(w, 400, "unsupported_bridge_version")
		return
	}
	tenant, err := strconv.ParseUint(r.Header.Get("X-AgentRAG-Tenant-ID"), 10, 64)
	id := nativeIdentity{ScopeIdentity: ScopeIdentity{TenantID: tenant, KnowledgeBaseID: r.Header.Get("X-AgentRAG-KB-ID")}, DocID: r.Header.Get("X-AgentRAG-Doc-ID")}
	if err != nil || !h.allowed[id.ScopeIdentity] || !safeID(id.DocID) {
		proxyError(w, 403, "native_identity_denied")
		return
	}
	h.mu.Lock()
	p := h.documents[id]
	if p == nil {
		if len(h.documents) >= proxyStateLimit {
			// Only durably saved, idle scopes may be released. Active and queued
			// requests retain their exact state until their ACK is committed.
			for key, candidate := range h.documents {
				if candidate.active == 0 && candidate.journal != nil && !candidate.poisoned {
					candidate.client.CloseIdleConnections()
					candidate.bulkClient.CloseIdleConnections()
					delete(h.documents, key)
					break
				}
			}
			if len(h.documents) >= proxyStateLimit {
				h.mu.Unlock()
				proxyError(w, 503, "capacity_reached")
				return
			}
		}
		p, err = NewScopeProxy(h.cfg.UpstreamURL, "http://127.0.0.1:8090", h.cfg.UpstreamKey, []ScopeBinding{{RouteID: "native", TenantID: id.TenantID, KnowledgeBaseID: id.KnowledgeBaseID, APIKey: h.cfg.APIKey}}, WithArtifactRoot(h.cfg.ArtifactRoot), func(p *ScopeProxy) error { p.nativeUpstream = true; return nil })
		if err != nil {
			h.mu.Unlock()
			proxyError(w, 503, "native_unavailable")
			return
		}
		p.scopes["native"].nativeDocument = id.DocID
		p.bulkClient.Timeout = h.cfg.BulkTimeout
		if h.db != nil {
			p.journal = newJournal(h.db, id, h.cfg)
			if err = p.journal.restore(r.Context(), p.scopes["native"]); err != nil {
				p.client.CloseIdleConnections()
				p.bulkClient.CloseIdleConnections()
				h.mu.Unlock()
				proxyError(w, 503, "state_restore_failed")
				return
			}
		}
		h.documents[id] = p
	}
	p.active++
	h.mu.Unlock()
	defer func() { h.mu.Lock(); p.active--; h.mu.Unlock() }()
	p.requestMu.Lock()
	defer p.requestMu.Unlock()
	if p.poisoned {
		proxyError(w, 503, "state_commit_failed")
		return
	}
	clone := r.Clone(r.Context())
	clone.URL.Path = strings.Replace(r.URL.Path, "/parser/native/v1/", "/parser/scopes/native/v1/", 1)
	// Metadata is bounded by ScopeProxy; ZIP bodies are bounded by proxyResultLimit.
	result := &nativeResponse{headers: make(http.Header)}
	p.ServeHTTP(result, clone)
	if p.journal != nil {
		// Persist already-observed effects even if the polling caller leaves.
		// Keep the operation bounded; a canceled GET must not poison a known
		// job capability and prevent the native client's detached DELETE.
		commitCtx, stopCommit := context.WithTimeout(context.WithoutCancel(r.Context()), 5*time.Second)
		err := p.journal.save(commitCtx, p.scopes["native"])
		stopCommit()
		if err != nil {
			p.poisoned = true
			proxyError(w, 503, "state_commit_failed")
			return
		}
	}
	body := result.body.Bytes()
	if result.code == 200 && r.Method == "POST" && r.URL.Path == "/parser/native/v1/uploads" {
		var pending map[string]any
		if json.Unmarshal(body, &pending) != nil {
			proxyError(w, 503, "invalid_upload_receipt")
			return
		}
		if upload, ok := pending["id"].(string); ok && safeID(upload) && pending["status"] == "pending" {
			pending["upload_url"] = "/parser/native/v1/uploads/" + upload + "/content"
			body, _ = json.Marshal(pending)
		} else {
			proxyError(w, 503, "invalid_upload_receipt")
			return
		}
	}
	for key, values := range result.Header() {
		w.Header()[key] = append([]string(nil), values...)
	}
	w.WriteHeader(result.code)
	_, _ = bytes.NewReader(body).WriteTo(w)
	if h.cfg.Report != nil && result.code >= 200 && result.code < 300 {
		h.cfg.Report("parser_native_request")
		if r.Method == "PUT" {
			h.cfg.Report("parser_native_preflight_passed")
		}
		if r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/parser/native/v1/files/") {
			file := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/parser/native/v1/files/"), "/content")
			s := p.scopes["native"]
			s.mu.Lock()
			_, archived := s.outputDocuments[file]
			s.mu.Unlock()
			if archived {
				h.cfg.Report("parser_native_artifact_committed")
			}
		}
	}
}

// Close releases only idle connections owned by this handler after HTTP drain.
func (h *NativeHandler) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, p := range h.documents {
		p.client.CloseIdleConnections()
		p.bulkClient.CloseIdleConnections()
	}
}

type nativeResponse struct {
	headers http.Header
	code    int
	body    bytes.Buffer
}

func (r *nativeResponse) Header() http.Header { return r.headers }
func (r *nativeResponse) WriteHeader(code int) {
	if r.code == 0 {
		r.code = code
	}
}
func (r *nativeResponse) Write(b []byte) (int, error) {
	if r.code == 0 {
		r.code = 200
	}
	if r.body.Len()+len(b) > proxyResultLimit {
		return 0, errors.New("native response limit")
	}
	return r.body.Write(b)
}
