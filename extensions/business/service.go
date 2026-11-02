// Package business implements M4 relationships and M5 feedback/revision workflows.
package business

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/contract"
	"github.com/ybochuan/LanRAG-Next/extensions/dependencies"
	"github.com/ybochuan/LanRAG-Next/extensions/lineage"
	"github.com/ybochuan/LanRAG-Next/extensions/provenance"
	"github.com/ybochuan/LanRAG-Next/extensions/retrieval"
	"github.com/ybochuan/LanRAG-Next/extensions/store"
	"github.com/ybochuan/LanRAG-Next/extensions/weknora"
)

var (
	ErrInvalid     = errors.New("invalid business command")
	ErrConflict    = errors.New("business revision or state conflict")
	ErrUnavailable = errors.New("business service unavailable")
)

// Payload contains business data only; native authorization supplies Command scope/actor.
type Payload struct {
	DocumentID         string   `json:"document_id,omitempty"`
	TargetDocumentID   string   `json:"target_document_id,omitempty"`
	EdgeType           string   `json:"edge_type,omitempty"`
	Text               string   `json:"text,omitempty"`
	Topic              string   `json:"topic,omitempty"`
	Risk               string   `json:"risk,omitempty"`
	Negative           bool     `json:"negative,omitempty"`
	SecondConfirmation bool     `json:"second_confirmation,omitempty"`
	GuidanceIDs        []string `json:"guidance_ids,omitempty"`
	Similarity         float64  `json:"similarity,omitempty"`
	Declaration        float64  `json:"declaration,omitempty"`
	Structure          float64  `json:"structure,omitempty"`
}

// Command is a native-authorized, idempotent business operation.
type Command struct {
	Scope            provenance.Scope `json:"scope"`
	ActorID          string           `json:"actor_id"`
	Action           string           `json:"action"`
	OperationID      string           `json:"operation_id"`
	ID               string           `json:"id,omitempty"`
	ExpectedRevision int64            `json:"expected_revision"`
	Payload          Payload          `json:"payload"`
	Page             int              `json:"page,omitempty"`
	PageSize         int              `json:"page_size,omitempty"`
}

// Result describes a committed governance state, not a current retrieval permit.
type Result struct {
	ID               string `json:"id"`
	Revision         int64  `json:"revision"`
	Status           string `json:"status"`
	PublicationKey   string `json:"publication_key,omitempty"`
	TargetDocumentID string `json:"target_document_id,omitempty"`
}

// Service shares the existing extension writer; it owns no goroutines or DB lifetime.
type Service struct {
	db       *store.DB
	registry *lineage.Registry
	now      func() time.Time
	remote   Remote
	verify   func(context.Context, provenance.Scope, string) (string, error)
}

// New uses real source snapshots when a Registry is configured.
func New(db *store.DB, registry *lineage.Registry, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	s := &Service{db: db, registry: registry, now: now}
	s.verify = func(ctx context.Context, scope provenance.Scope, doc string) (string, error) {
		if registry == nil {
			return "", ErrUnavailable
		}
		v, e := registry.Refresh(ctx, scope, doc)
		if e != nil {
			return "", e
		}
		policy, e := retrieval.NewPolicy(db, scope.TenantID, []string{scope.KnowledgeBaseID})
		if e != nil {
			return "", e
		}
		excluded, e := policy.WithSources(registry).ExcludedDocumentIDs(ctx, contract.BeforeRetrieveRequest{Context: contract.Context{RequestID: newID(), SessionID: "business", Mode: "rag", TenantID: scope.TenantID, KnowledgeBaseIDs: []string{scope.KnowledgeBaseID}, KnowledgeIDs: []string{doc}}})
		if e != nil || len(excluded) != 0 {
			return "", ErrConflict
		}
		return v.ID, e
	}
	return s
}
func args(s provenance.Scope, tail ...any) []any {
	return append([]any{strconv.FormatUint(s.TenantID, 10), s.KnowledgeBaseID}, tail...)
}
func digest(v any) string {
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func newID() string {
	var b [16]byte
	if _, e := rand.Read(b[:]); e != nil {
		return ""
	}
	return hex.EncodeToString(b[:])
}
func validID(v string) bool {
	return weknora.ValidID(v) && len(v) <= 128 && strings.TrimSpace(v) == v && !strings.ContainsAny(v, "\x00\r\n\t ")
}
func (s *Service) transact(ctx context.Context, c Command, f func(*sql.Tx) (Result, error)) (out Result, err error) {
	if s == nil || s.db == nil {
		return out, ErrUnavailable
	}
	if c.Scope.TenantID == 0 || !validID(c.Scope.KnowledgeBaseID) || !validID(c.ActorID) || !validID(c.OperationID) || c.ExpectedRevision < 0 {
		return out, ErrInvalid
	}
	hash := digest(c)
	err = s.db.Write(ctx, func(tx *sql.Tx) error {
		var old string
		var body []byte
		e := tx.QueryRowContext(ctx, `SELECT payload_hash,result_json FROM ext_business_receipts WHERE tenant_id=? AND knowledge_base_id=? AND operation_id=?`, args(c.Scope, c.OperationID)...).Scan(&old, &body)
		if e == nil {
			if old != hash {
				return ErrConflict
			}
			if json.Unmarshal(body, &out) != nil {
				return ErrUnavailable
			}
			return nil
		}
		if !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		out, e = f(tx)
		if e != nil {
			return e
		}
		body, e = json.Marshal(out)
		if e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO ext_business_receipts VALUES(?,?,?,?,?)`, args(c.Scope, c.OperationID, hash, string(body))...); e != nil {
			return e
		}
		payload, _ := json.Marshal(c)
		_, e = tx.ExecContext(ctx, `INSERT INTO ext_business_audit(tenant_id,knowledge_base_id,operation_id,actor_id,action,aggregate_id,revision,status,occurred_at,payload_json) VALUES(?,?,?,?,?,?,?,?,?,?)`, args(c.Scope, c.OperationID, c.ActorID, c.Action, out.ID, out.Revision, out.Status, s.now().UTC().Format(time.RFC3339Nano), string(payload))...)
		return e
	})
	if err != nil {
		return Result{}, err
	}
	return
}
func (s *Service) current(ctx context.Context, tx *sql.Tx, scope provenance.Scope, doc, version string) error {
	state, _, e := store.ReadLifecycleCurrentTx(ctx, tx, scope.TenantID, scope.KnowledgeBaseID, doc)
	if e != nil || state != "active" {
		return ErrConflict
	}
	var head string
	if e = tx.QueryRowContext(ctx, `SELECT current_version_node_id FROM ext_document_version_family WHERE tenant_id=? AND knowledge_base_id=? AND document_id=?`, args(scope, doc)...).Scan(&head); e != nil || head != version {
		return ErrConflict
	}
	a, e := dependencies.LoadTx(ctx, tx, dependencies.Scope{TenantID: scope.TenantID, KnowledgeBaseID: scope.KnowledgeBaseID, DocumentID: doc})
	if e != nil || a.Invalid(s.now()) {
		return ErrConflict
	}
	var from, until sql.NullString
	e = tx.QueryRowContext(ctx, `SELECT effective_from,effective_until FROM ext_document_annotations WHERE tenant_id=? AND knowledge_base_id=? AND document_id=?`, args(scope, doc)...).Scan(&from, &until)
	if e != nil && !errors.Is(e, sql.ErrNoRows) {
		return e
	}
	for _, date := range []struct {
		v     sql.NullString
		start bool
	}{{from, true}, {until, false}} {
		if date.v.Valid {
			t, e := time.Parse(time.RFC3339Nano, date.v.String)
			if e != nil || date.start && s.now().Before(t) || !date.start && s.now().After(t) {
				return ErrConflict
			}
		}
	}
	return nil
}

// Execute dispatches writes without touching native documents or search indexes.
func (s *Service) Execute(ctx context.Context, c Command) (Result, error) {
	switch c.Action {
	case "relation_declare", "relation_confirm", "relation_revoke":
		return s.relation(ctx, c)
	case "feedback_create", "feedback_approve", "feedback_reject", "feedback_quarantine", "feedback_activate", "feedback_withdraw", "guidance_seed", "guidance_quarantine_topic":
		return s.feedback(ctx, c)
	case "revision_create", "revision_approve", "revision_reject":
		return s.revision(ctx, c)
	default:
		return Result{}, ErrInvalid
	}
}
