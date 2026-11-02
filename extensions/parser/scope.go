package parser

import (
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ScopeBinding is an operator-configured endpoint identity, not native KB authorization.
type ScopeBinding struct {
	RouteID         string `json:"route_id"`          // Opaque single path segment.
	TenantID        uint64 `json:"tenant_id"`         // Explicit configured tenant.
	KnowledgeBaseID string `json:"knowledge_base_id"` // Explicit configured KB.
	APIKey          string `json:"-"`                 // Separate client credential, never forwarded upstream.
}

func (ScopeBinding) String() string     { return "parser scope binding (redacted)" }
func (s ScopeBinding) GoString() string { return s.String() }

// ScopeProxy enforces preflight and scoped V1 capabilities. Its standalone
// constructor is process-local; the native Host attaches a durable journal.
type ScopeProxy struct {
	upstream, origin, upstreamKey string
	client                        *http.Client
	bulkClient                    *http.Client
	scopes                        map[string]*scopeState
	artifacts                     *artifactStore
	nativeUpstream                bool // Only operator-configured NativeHandler may opt in.
	journal                       *parserJournal
	requestMu                     sync.Mutex // Native document requests, including ACK persistence.
	active                        int        // Guarded by NativeHandler.mu; queued requests count too.
	poisoned                      bool       // A failed durable write requires handler restart.
}

func (*ScopeProxy) String() string     { return "parser scope proxy (redacted)" }
func (p *ScopeProxy) GoString() string { return p.String() }

// ScopeIdentity is the immutable operator assertion used by future KB-specific
// policy. It does not prove a native document's ownership or permissions.
type ScopeIdentity struct {
	TenantID        uint64 `json:"tenant_id"`
	KnowledgeBaseID string `json:"knowledge_base_id"`
}

// ResolveScope returns a copy of the configured identity after authenticating
// its own credential; a different scope's credential yields no identity.
func (p *ScopeProxy) ResolveScope(routeID, apiKey string) (ScopeIdentity, error) {
	s, err := p.resolve(routeID, "Bearer "+apiKey)
	if err != nil {
		return ScopeIdentity{}, err
	}
	return s.identity, nil
}
func (p *ScopeProxy) resolve(routeID, authorization string) (*scopeState, error) {
	if p == nil {
		return nil, errors.New("parser scope denied")
	}
	s := p.scopes[routeID]
	auth := sha256.Sum256([]byte(authorization))
	if s == nil || subtle.ConstantTimeCompare(auth[:], s.auth[:]) != 1 {
		return nil, errors.New("parser scope denied")
	}
	return s, nil
}

type scopeState struct {
	nativeDocument  string // Trusted native gateway identity, bound before complete ACK.
	identity        ScopeIdentity
	auth            [32]byte
	mu              sync.Mutex
	uploads         map[string]*sourceUpload
	files           map[string]bool         // Present: readable capability; true: byte-verified input, false: output only.
	hashes          map[string]string       // Physically verified source SHA256, never metadata alone.
	pdfs            map[string]PDFPreflight // Only from physically verified source bytes.
	documents       map[string]string       // Trusted coordinator's source -> native doc_id assertion.
	jobDocuments    map[string]artifactRef
	outputDocuments map[string]artifactRef // Only a completed job's ZIP output.
	jobs            map[string]bool
	jobKeys         map[string]string
	requests        map[[32]byte]*jobReceipt
}
type sourceUpload struct {
	size                                    int64
	hash, backendID, backendFile, uploadURL string
	busy, verified                          bool
	declaredPDF                             bool
	preflight                               *PDFPreflight
}
type jobReceipt struct {
	busy   bool
	status int
	body   []byte
}

// NewScopeProxy accepts fixed IPv4 loopback origins and immutable bindings.
// Cached sources still require uploaded bytes; job receipts survive only this process.
func NewScopeProxy(upstream, publicOrigin, upstreamKey string, bindings []ScopeBinding, options ...ProxyOption) (*ScopeProxy, error) {
	if !loopbackOrigin(publicOrigin) || len(bindings) == 0 || len(bindings) > 128 {
		return nil, errors.New("invalid parser proxy configuration")
	}
	p := &ScopeProxy{upstream: strings.TrimSuffix(upstream, "/"), origin: strings.TrimSuffix(publicOrigin, "/"), upstreamKey: upstreamKey, scopes: map[string]*scopeState{}}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("nil proxy option")
		}
		if err := option(p); err != nil {
			return nil, err
		}
	}
	if !loopbackOrigin(upstream) && !p.nativeUpstream {
		return nil, errors.New("invalid parser upstream origin")
	}
	keys := map[[32]byte]bool{}
	for _, b := range bindings {
		auth := sha256.Sum256([]byte("Bearer " + b.APIKey))
		if !safeID(b.RouteID) || b.TenantID == 0 || !safeID(b.KnowledgeBaseID) || len(b.APIKey) < 16 || len(b.APIKey) > 256 || strings.TrimSpace(b.APIKey) != b.APIKey || p.scopes[b.RouteID] != nil || keys[auth] {
			return nil, errors.New("invalid parser scope binding")
		}
		keys[auth] = true
		p.scopes[b.RouteID] = &scopeState{identity: ScopeIdentity{TenantID: b.TenantID, KnowledgeBaseID: b.KnowledgeBaseID}, auth: auth, uploads: map[string]*sourceUpload{}, files: map[string]bool{}, hashes: map[string]string{}, pdfs: map[string]PDFPreflight{}, documents: map[string]string{}, jobDocuments: map[string]artifactRef{}, outputDocuments: map[string]artifactRef{}, jobs: map[string]bool{}, jobKeys: map[string]string{}, requests: map[[32]byte]*jobReceipt{}}
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	p.client = &http.Client{Timeout: 30 * time.Second, Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	bulk := *p.client
	bulk.Timeout = 1000 * time.Second
	p.bulkClient = &bulk
	return p, nil
}
func loopbackOrigin(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "http" || u.Hostname() != "127.0.0.1" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.RawPath != "" || (u.Path != "" && u.Path != "/") {
		return false
	}
	if u.Port() != "" {
		port, err := strconv.Atoi(u.Port())
		return err == nil && port > 0 && port <= 65535
	}
	return true
}
func safeID(s string) bool {
	if s == "" || len(s) > 256 || s == "." || s == ".." {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' || r == '.') {
			return false
		}
	}
	return true
}
