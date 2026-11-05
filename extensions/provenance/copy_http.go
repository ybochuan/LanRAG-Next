package provenance

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

// HTTPRequest is exclusively a server-to-server DTO. Browser/model arguments
// cannot attest native authorization or enumerate a complete input set.
type HTTPRequest struct {
	RequestID             string           `json:"request_id"`
	Scope                 Scope            `json:"scope"`
	OperationID           string           `json:"operation_id,omitempty"`
	Action                string           `json:"action,omitempty"`
	Identity              *RunIdentity     `json:"identity,omitempty"`
	Handle                *RunHandle       `json:"handle,omitempty"`
	Step                  *StepRequest     `json:"step,omitempty"`
	Input                 *InputRequest    `json:"input,omitempty"`
	Result                *StepResult      `json:"result,omitempty"`
	Finish                *FinishRequest   `json:"finish,omitempty"`
	Producer              *ProducerRequest `json:"producer,omitempty"`
	Produced              *ProducerResult  `json:"produced,omitempty"`
	Save                  *SaveRequest     `json:"save,omitempty"`
	Read                  *ReadRequest     `json:"read,omitempty"`
	Binding               *Binding         `json:"binding,omitempty"`
	CopyID                string           `json:"copy_id,omitempty"`
	AuthorizedDocumentIDs []string         `json:"authorized_document_ids,omitempty"`
	Guidance              *GuidanceRequest `json:"guidance,omitempty"`
}

// HTTPServer serves proof APIs on the existing loopback listener. A missing
// worker DB/provider/key cannot fall back to an allow policy.
type HTTPServer struct {
	runs   *Runs
	ledger *Ledger
	key    string
	scopes map[Scope]bool
}

// NewHTTPServer installs no fixtures and owns no DB or goroutine lifetime.
func NewHTTPServer(runs *Runs, ledger *Ledger, key string, scopes []Scope) *HTTPServer {
	s := &HTTPServer{runs: runs, ledger: ledger, key: key, scopes: map[Scope]bool{}}
	for _, scope := range scopes {
		if validScope(scope) {
			s.scopes[scope] = true
		}
	}
	return s
}

// ServeHTTP authenticates before decoding any source identity.
func (s *HTTPServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	fail := func(status int) {
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]any{"success": false, "error": "provenance_unavailable"})
	}
	if s == nil || !s.runs.available() || s.ledger == nil || len(s.key) < 32 {
		fail(503)
		return
	}
	if r.Method != http.MethodPost {
		fail(405)
		return
	}
	if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+s.key)) != 1 {
		fail(401)
		return
	}
	if r.Header.Get("X-AgentRAG-Bridge-Version") != "1" {
		fail(400)
		return
	}
	var q HTTPRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 128<<10))
	dec.DisallowUnknownFields()
	if dec.Decode(&q) != nil || dec.Decode(new(any)) != io.EOF || !id(q.RequestID) || !validScope(q.Scope) {
		fail(400)
		return
	}
	if !s.scopes[q.Scope] {
		fail(403)
		return
	}
	if q.Handle != nil && q.Handle.Scope != q.Scope || q.Identity != nil && q.Identity.Scope != q.Scope || q.Read != nil && q.Read.Scope != q.Scope || q.Save != nil && q.Save.Scope != q.Scope {
		fail(403)
		return
	}
	data, e := s.execute(r.Context(), r.URL.Path, q)
	if e != nil {
		status := 503
		switch {
		case errors.Is(e, ErrInvalid):
			status = 400
		case errors.Is(e, ErrEvidence):
			status = 403
		case errors.Is(e, ErrConflict):
			status = 409
		}
		fail(status)
		return
	}
	_ = json.NewEncoder(w).Encode(struct {
		Success   bool   `json:"success"`
		RequestID string `json:"request_id"`
		Scope     Scope  `json:"scope"`
		Data      any    `json:"data"`
	}{true, q.RequestID, q.Scope, data})
}

func (s *HTTPServer) execute(ctx context.Context, path string, q HTTPRequest) (any, error) {
	if path == "/provenance/runs/begin" {
		if q.Identity == nil {
			return nil, ErrInvalid
		}
		return s.runs.Begin(ctx, *q.Identity, q.OperationID)
	}
	if path == "/provenance/copies/resolve" {
		if q.Binding == nil || !validBinding(*q.Binding) {
			return nil, ErrInvalid
		}
		return s.resolve(ctx, q.Scope, q.CopyID, *q.Binding)
	}
	if path == "/provenance/copies/check" {
		if q.Read == nil {
			return nil, ErrInvalid
		}
		m, e := s.ledger.Snapshot(ctx, q.Scope, q.Read.CopyID)
		if e != nil || m.Revision != q.Read.Revision || m.Binding != q.Read.Binding || !authorizedSources(m.Sources, q.AuthorizedDocumentIDs) {
			return nil, ErrEvidence
		}
		d, e := s.ledger.CheckRead(ctx, *q.Read)
		if e != nil || !d.Allowed {
			return nil, ErrEvidence
		}
		return d, nil
	}
	if q.Handle == nil {
		return nil, ErrInvalid
	}
	switch path {
	case "/bridge/before-generate":
		if q.Guidance != nil {
			return s.runs.PrepareGuidance(ctx, *q.Handle, *q.Guidance)
		}
	case "/provenance/runs/steps":
		if q.Action == "begin" && q.Step != nil {
			return s.runs.BeginStep(ctx, *q.Handle, *q.Step)
		}
		if q.Action == "finish" && q.Result != nil {
			return struct{}{}, s.runs.FinishStep(ctx, *q.Handle, *q.Result)
		}
	case "/provenance/runs/inputs":
		if q.Input != nil {
			return struct{}{}, s.runs.RecordInput(ctx, *q.Handle, *q.Input)
		}
	case "/provenance/runs/finish":
		if q.Action == "inspect" {
			return s.runs.Inspect(ctx, *q.Handle)
		}
		if q.Finish != nil {
			return s.runs.Finish(ctx, *q.Handle, *q.Finish)
		}
	case "/provenance/producers":
		if q.Action == "reserve" && q.Producer != nil {
			return s.runs.ReserveProducer(ctx, *q.Handle, *q.Producer)
		}
		if q.Action == "commit" && q.Produced != nil {
			return struct{}{}, s.runs.CommitProducer(ctx, *q.Handle, *q.Produced)
		}
		if q.Action == "publish" && q.Save != nil {
			v, e := s.runs.Inspect(ctx, *q.Handle)
			if e != nil || v.State != "sealed" {
				return nil, ErrEvidence
			}
			p, e := s.runs.SealedCopy(ctx, q.Scope, q.Save.CopyID, q.Save.Binding, q.Save.EvidenceID)
			if e != nil || p.RunID != q.Handle.RunID {
				return nil, ErrEvidence
			}
			return s.ledger.Save(ctx, *q.Save)
		}
	}
	return nil, ErrInvalid
}

func authorizedSources(refs []SourceRef, ids []string) bool {
	if len(refs) == 0 || len(ids) == 0 || len(ids) > 256 {
		return false
	}
	allowed := map[string]bool{}
	for _, doc := range ids {
		if !id(doc) || allowed[doc] {
			return false
		}
		allowed[doc] = true
	}
	for _, ref := range refs {
		if !allowed[ref.DocumentID] {
			return false
		}
	}
	return true
}

// resolve discloses only exact-owner/binding metadata to the trusted native
// facade; it grants no content access. Ambiguous aliases are refused.
func (s *HTTPServer) resolve(ctx context.Context, scope Scope, copyID string, b Binding) (Manifest, error) {
	if copyID != "" {
		m, e := s.ledger.Snapshot(ctx, scope, copyID)
		if e != nil || m.Status != "ready" || m.Binding != b {
			return Manifest{}, ErrEvidence
		}
		return m, nil
	}
	var matches []Manifest
	e := s.ledger.db.View(ctx, func(tx *sql.Tx) error {
		rows, e := tx.QueryContext(ctx, `SELECT m.payload_json FROM ext_provenance_copy_heads h JOIN ext_provenance_manifests m ON m.tenant_id=h.tenant_id AND m.knowledge_base_id=h.knowledge_base_id AND m.copy_id=h.copy_id AND m.revision=h.revision WHERE h.tenant_id=? AND h.knowledge_base_id=? AND m.status='ready' AND json_extract(m.payload_json,'$.binding.user_id')=? AND json_extract(m.payload_json,'$.binding.session_id')=? AND json_extract(m.payload_json,'$.binding.resource_id')=? AND json_extract(m.payload_json,'$.binding.content_version')=? AND json_extract(m.payload_json,'$.binding.sha256')=? LIMIT 2`, args(scope, b.UserID, b.SessionID, b.ResourceID, b.ContentVersion, b.SHA256)...)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var body []byte
			if e = rows.Scan(&body); e != nil {
				return e
			}
			var m Manifest
			if json.Unmarshal(body, &m) != nil || m.Scope != scope || m.Binding != b || m.Status != "ready" {
				return ErrUnavailable
			}
			matches = append(matches, m)
		}
		return rows.Err()
	})
	if e != nil || len(matches) != 1 {
		return Manifest{}, ErrEvidence
	}
	return matches[0], nil
}
