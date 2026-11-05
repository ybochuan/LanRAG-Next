// Package management adapts authenticated, scoped UI operations to extension services.
package management

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/business"
	"github.com/ybochuan/LanRAG-Next/extensions/dependencies"
	"github.com/ybochuan/LanRAG-Next/extensions/lifecycle"
	"github.com/ybochuan/LanRAG-Next/extensions/lineage"
	"github.com/ybochuan/LanRAG-Next/extensions/outbox"
	"github.com/ybochuan/LanRAG-Next/extensions/review"
	"github.com/ybochuan/LanRAG-Next/extensions/store"
)

// Server serves only server-authorized scopes; its credential never reaches a browser.
type Server struct {
	db       *store.DB
	key      string
	scopes   map[lifecycle.PolicyScope]bool
	sources  *lineage.Registry
	business *business.Service
	nativeDB string
	titles   func(context.Context, string, []string) (map[string]string, error)
}

// WithNativeSQLite supplies the read-only join and its scoped API fallback.
func (s *Server) WithNativeSQLite(path string, titles func(context.Context, string, []string) (map[string]string, error)) *Server {
	s.nativeDB, s.titles = path, titles
	return s
}

// WithBusiness shares the worker's real source and native publisher adapters.
func (s *Server) WithBusiness(b *business.Service) *Server { s.business = b; return s }

// WithSources enables explicit declarations using the real native snapshot reader.
func (s *Server) WithSources(r *lineage.Registry) *Server { s.sources = r; return s }

// New creates a disabled (503) management adapter when no strong key is configured.
func New(db *store.DB, key string, scopes []lifecycle.PolicyScope) *Server {
	s := &Server{db: db, key: key, scopes: map[lifecycle.PolicyScope]bool{}}
	for _, v := range scopes {
		s.scopes[v] = true
	}
	return s
}

// Request carries authority supplied exclusively by the native facade.
type Request struct {
	Action           string                `json:"action"`
	TenantID         uint64                `json:"tenant_id"`
	KnowledgeBaseID  string                `json:"knowledge_base_id"`
	DocumentID       string                `json:"document_id,omitempty"`
	Page             int                   `json:"page,omitempty"`
	PageSize         int                   `json:"page_size,omitempty"`
	State            string                `json:"state,omitempty"`
	Title            string                `json:"title,omitempty"`
	CanWrite         bool                  `json:"can_write"`
	ExpectedRevision int64                 `json:"expected_revision"`
	Annotations      lifecycle.Annotations `json:"annotations"`
	Rules            lifecycle.Rules       `json:"rules"`
	ActorID          string                `json:"actor_id,omitempty"`
	OperationID      string                `json:"operation_id,omitempty"`
	Parents          []string              `json:"parents,omitempty"`
	Business         json.RawMessage       `json:"business,omitempty"`
}

func writeError(w http.ResponseWriter, status int, code string) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"success": false, "error": map[string]string{"code": code, "message": code}})
}

// ServeHTTP rejects unsupported versions, unauthenticated callers and foreign scopes before DB access.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if s.db == nil || len(s.key) < 32 {
		writeError(w, 503, "management_not_configured")
		return
	}
	if r.Method != "POST" {
		writeError(w, 405, "method_not_allowed")
		return
	}
	if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+s.key)) != 1 {
		writeError(w, 401, "unauthorized")
		return
	}
	if r.Header.Get("X-AgentRAG-Bridge-Version") != "1" {
		writeError(w, 400, "unsupported_version")
		return
	}
	var req Request
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil || dec.Decode(new(any)) != io.EOF {
		writeError(w, 400, "invalid_request")
		return
	}
	if !s.scopes[lifecycle.PolicyScope{TenantID: req.TenantID, KnowledgeBaseID: req.KnowledgeBaseID}] {
		writeError(w, 403, "scope_not_configured")
		return
	}
	mutation := req.Action == "put_annotations" || req.Action == "put_rules" || req.Action == "put_supplements" || strings.HasPrefix(req.Action, "business_") && !businessRead(req.Action)
	if mutation && !req.CanWrite {
		writeError(w, 403, "read_only")
		return
	}
	if req.ExpectedRevision < 0 || len(req.DocumentID) > 128 {
		writeError(w, 400, "invalid_request")
		return
	}
	data, err := s.execute(r.Context(), req)
	if err != nil {
		status, code := 400, "invalid_configuration"
		if errors.Is(err, lifecycle.ErrConfigurationConflict) || errors.Is(err, lineage.ErrConflict) || errors.Is(err, business.ErrConflict) {
			status, code = 409, "revision_conflict"
		}
		if errors.Is(err, errNotFound) {
			status, code = 404, "document_not_found"
		}
		if errors.Is(err, errDatabase) || errors.Is(err, lineage.ErrUnavailable) || errors.Is(err, business.ErrUnavailable) {
			status, code = 503, "management_unavailable"
		}
		writeError(w, status, code)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "tenant_id": req.TenantID, "knowledge_base_id": req.KnowledgeBaseID, "data": data})
}

var errNotFound = errors.New("document not found")
var errDatabase = errors.New("management database unavailable")

func (s *Server) execute(ctx context.Context, r Request) (any, error) {
	if strings.HasPrefix(r.Action, "business_") {
		return s.executeBusiness(ctx, r)
	}
	c := lifecycle.NewConfiguration(s.db)
	m := lifecycle.NewMachine(s.db, time.Now)
	scope := lifecycle.Scope{TenantID: r.TenantID, KnowledgeBaseID: r.KnowledgeBaseID, DocumentID: r.DocumentID}
	switch r.Action {
	case "supplements", "put_supplements":
		snap, e := m.Snapshot(ctx, scope)
		if e != nil {
			return nil, errDatabase
		}
		if snap.Current.State == "" {
			return nil, errNotFound
		}
		supplements := lineage.NewSupplements(s.db, s.sources, time.Now)
		ds := dependencies.Scope{TenantID: r.TenantID, KnowledgeBaseID: r.KnowledgeBaseID, DocumentID: r.DocumentID}
		var receipt *lineage.SupplementReceipt
		evaluation := "unchanged"
		if r.Action == "put_supplements" {
			if s.sources == nil {
				return nil, errDatabase
			}
			saved, e := supplements.Put(ctx, lineage.SupplementCommand{Scope: ds, ActorID: r.ActorID, OperationID: r.OperationID, ExpectedRevision: r.ExpectedRevision, Parents: r.Parents})
			if e != nil {
				return nil, e
			}
			receipt = &saved
			evaluation = "evaluated"
			if e = m.Reevaluate(ctx, scope); e != nil {
				evaluation = "pending"
			}
		}
		view, e := supplements.Get(ctx, ds)
		if e != nil {
			return nil, errDatabase
		}
		return map[string]any{"supplements": view, "receipt": receipt, "evaluation_status": evaluation}, nil
	case "overview":
		return s.overview(ctx, r)
	case "document", "put_annotations":
		snap, err := m.Snapshot(ctx, scope)
		if err != nil {
			return nil, errDatabase
		}
		if snap.Current.State == "" {
			return nil, errNotFound
		}
		evaluation := "unchanged"
		if r.Action == "put_annotations" {
			if err = c.PutManagementAnnotations(ctx, scope, r.Annotations, r.ExpectedRevision, time.Now().UTC()); err != nil {
				return nil, err
			}
			evaluation = "evaluated"
			if err = m.Reevaluate(ctx, scope); err != nil {
				evaluation = "pending"
			}
			snap, err = m.Snapshot(ctx, scope)
			if err != nil {
				return nil, errDatabase
			}
		}
		events, truncated, err := s.events(ctx, scope)
		if err != nil {
			return nil, err
		}
		return map[string]any{"snapshot": snap, "events": events, "events_truncated": truncated, "evaluation_status": evaluation}, nil
	case "rules", "put_rules":
		if r.Action == "put_rules" {
			if err := c.PutRules(ctx, lifecycle.PolicyScope{TenantID: r.TenantID, KnowledgeBaseID: r.KnowledgeBaseID}, r.Rules, r.ExpectedRevision); err != nil {
				return nil, err
			}
		}
		return s.rules(ctx, r)
	case "reviews":
		alerts, err := review.NewService(s.db, time.Now).List(ctx, outbox.Scope{TenantID: r.TenantID, KnowledgeBaseID: r.KnowledgeBaseID}, 1000)
		if err != nil {
			return nil, errDatabase
		}
		return map[string]any{"alerts": alerts, "truncated": len(alerts) == 1000}, nil
	default:
		return nil, errors.New("invalid action")
	}
}
