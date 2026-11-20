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
	DocumentID            string            `json:"document_id,omitempty"`
	FamilyLabel           string            `json:"family_label,omitempty"`
	BusinessFamilyID      string            `json:"business_family_id,omitempty"`
	FamilyRevision        int64             `json:"family_revision,omitempty"`
	LegacyFamilyID        string            `json:"legacy_family_id,omitempty"`
	BackfillFingerprint   string            `json:"backfill_fingerprint,omitempty"`
	ReplacementIDs        []string          `json:"replacement_ids,omitempty"`
	HistoryFingerprint    string            `json:"history_fingerprint,omitempty"`
	TargetKnowledgeBaseID string            `json:"target_knowledge_base_id,omitempty"`
	TargetDocumentID      string            `json:"target_document_id,omitempty"`
	SessionID             string            `json:"session_id,omitempty"`
	MessageID             string            `json:"message_id,omitempty"`
	FeedbackContext       *FeedbackContext  `json:"feedback_context,omitempty"`
	EdgeType              string            `json:"edge_type,omitempty"`
	Text                  string            `json:"text,omitempty"`
	Reason                string            `json:"reason,omitempty"`
	HoldDays              int               `json:"hold_days,omitempty"`
	SuggestionPolicy      *SuggestionPolicy `json:"suggestion_policy,omitempty"`
	Topic                 string            `json:"topic,omitempty"`
	Risk                  string            `json:"risk,omitempty"`
	Negative              bool              `json:"negative,omitempty"`
	SecondConfirmation    bool              `json:"second_confirmation,omitempty"`
	SampleDecision        string            `json:"sample_decision,omitempty"`
	ReviewNote            string            `json:"review_note,omitempty"`
	GuidanceIDs           []string          `json:"guidance_ids,omitempty"`
	SplitParts            []SplitPartInput  `json:"split_parts,omitempty"`
	EffectiveAt           *time.Time        `json:"effective_at,omitempty"`
	SourceClause          *ClauseAnchor     `json:"source_clause,omitempty"`
	TargetClause          *ClauseAnchor     `json:"target_clause,omitempty"`
	AppliesWhen           string            `json:"applies_when,omitempty"`
	Resolution            string            `json:"resolution,omitempty"`
	Similarity            float64           `json:"similarity,omitempty"`
	Declaration           float64           `json:"declaration,omitempty"`
	Structure             float64           `json:"structure,omitempty"`
	CitedChunkIDs         []string          `json:"cited_chunk_ids,omitempty"`
	Decision              string            `json:"decision,omitempty"`
}

// FeedbackContext contains the excerpts a submitter explicitly shares with reviewers.
// Only the sealed run supplies authoritative citation identity.
type FeedbackContext struct {
	Rating             string `json:"rating"`
	Question           string `json:"question,omitempty"`
	AnswerExcerpt      string `json:"answer_excerpt,omitempty"`
	SuggestedRevision  string `json:"suggested_revision,omitempty"`
	QuestionTruncated  bool   `json:"question_truncated,omitempty"`
	AnswerTruncated    bool   `json:"answer_truncated,omitempty"`
	SnapshotRestricted bool   `json:"snapshot_restricted,omitempty"`
	CommentPresent     bool   `json:"comment_present,omitempty"`
}

// Command is a native-authorized, idempotent business operation.
type Command struct {
	Scope                         provenance.Scope `json:"scope"`
	ActorID                       string           `json:"actor_id"`
	Action                        string           `json:"action"`
	OperationID                   string           `json:"operation_id"`
	ID                            string           `json:"id,omitempty"`
	ExpectedRevision              int64            `json:"expected_revision"`
	Payload                       Payload          `json:"payload"`
	Page                          int              `json:"page,omitempty"`
	PageSize                      int              `json:"page_size,omitempty"`
	ListStatus                    string           `json:"-"`
	ListEdgeType                  string           `json:"-"`
	IncludeSupplementDeclarations bool             `json:"-"`
	IncludeFeedbackContext        bool             `json:"-"`
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
	db             *store.DB
	registry       *lineage.Registry
	now            func() time.Time
	remote         Remote
	familyEmbedder *LocalFamilyEmbedder
	verify         func(context.Context, provenance.Scope, string) (string, error)
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
		excluded, e := policy.WithSources(registry).WithNow(s.now).ExcludedDocumentIDs(ctx, contract.BeforeRetrieveRequest{Context: contract.Context{RequestID: newID(), SessionID: "business", Mode: "rag", TenantID: scope.TenantID, KnowledgeBaseIDs: []string{scope.KnowledgeBaseID}, KnowledgeIDs: []string{doc}}})
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

// Read receipts before any remote observation that may fail after a successful command.
func (s *Service) replay(ctx context.Context, c Command) (Result, bool, error) {
	var result Result
	var hash string
	var body []byte
	err := s.db.View(ctx, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `SELECT payload_hash,result_json FROM ext_business_receipts
			WHERE tenant_id=? AND knowledge_base_id=? AND operation_id=?`, args(c.Scope, c.OperationID)...).Scan(&hash, &body)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return Result{}, false, nil
	}
	if err != nil {
		return Result{}, false, err
	}
	if hash != digest(c) {
		return Result{}, false, ErrConflict
	}
	if json.Unmarshal(body, &result) != nil {
		return Result{}, false, ErrUnavailable
	}
	return result, true, nil
}

func (s *Service) replayAfterObservationError(ctx context.Context, c Command, cause error) (Result, error) {
	if result, replayed, err := s.replay(ctx, c); replayed || err != nil {
		return result, err
	}
	return Result{}, cause
}

func (s *Service) transact(ctx context.Context, c Command, f func(*sql.Tx) (Result, error)) (out Result, err error) {
	if s == nil || s.db == nil {
		return out, ErrUnavailable
	}
	tenantQA := c.Scope.KnowledgeBaseID == "" && (c.Action == "qa_feedback_submit" || c.Action == "qa_feedback_review")
	if c.Scope.TenantID == 0 || (!tenantQA && !validID(c.Scope.KnowledgeBaseID)) || !validID(c.ActorID) || !validID(c.OperationID) || c.ExpectedRevision < 0 {
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
		audit := c
		audit.Payload.FeedbackContext = nil
		if c.Action == "qa_feedback_submit" {
			audit.Payload.Text = ""
		}
		payload, _ := json.Marshal(audit)
		_, e = tx.ExecContext(ctx, `INSERT INTO ext_business_audit(tenant_id,knowledge_base_id,operation_id,actor_id,action,aggregate_id,revision,status,occurred_at,payload_json) VALUES(?,?,?,?,?,?,?,?,?,?)`, args(c.Scope, c.OperationID, c.ActorID, c.Action, out.ID, out.Revision, out.Status, s.now().UTC().Format(time.RFC3339Nano), string(payload))...)
		return e
	})
	if err != nil {
		return Result{}, err
	}
	return
}
func (s *Service) current(ctx context.Context, tx *sql.Tx, scope provenance.Scope, doc, version string) error {
	return s.currentWithPublicationGate(ctx, tx, scope, doc, version, store.PublicationTargetPendingSQL)
}

func (s *Service) currentWithPublicationGate(ctx context.Context, tx *sql.Tx, scope provenance.Scope, doc, version, gateSQL string) error {
	verify := s.currentVersionEvidence
	if s.registry != nil {
		verify = s.currentEnabled
	}
	if err := verify(ctx, tx, scope, doc, version); err != nil {
		return err
	}
	var held bool
	err := tx.QueryRowContext(ctx, `SELECT (`+store.RepealDueSQL+` OR `+gateSQL+`)
		FROM ext_lifecycle_current c WHERE c.tenant_id=? AND c.knowledge_base_id=? AND c.document_id=?`,
		args(scope, doc)...).Scan(&held)
	if err != nil {
		return err
	}
	if held {
		return ErrConflict
	}
	return nil
}

func (s *Service) currentVersionEvidence(ctx context.Context, tx *sql.Tx, scope provenance.Scope, doc, version string) error {
	state, _, e := store.ReadLifecycleCurrentTx(ctx, tx, scope.TenantID, scope.KnowledgeBaseID, doc)
	if e != nil || state != "active" {
		return ErrConflict
	}
	var head string
	if e = tx.QueryRowContext(ctx, `SELECT current_version_node_id FROM ext_document_version_family WHERE tenant_id=? AND knowledge_base_id=? AND document_id=?`, args(scope, doc)...).Scan(&head); e != nil || head != version {
		return ErrConflict
	}
	a, e := dependencies.LoadTx(ctx, tx, dependencies.Scope{TenantID: scope.TenantID, KnowledgeBaseID: scope.KnowledgeBaseID, DocumentID: doc})
	if e != nil || a.ReadBlocked(s.now()) {
		return ErrConflict
	}
	var from, until sql.NullString
	e = tx.QueryRowContext(ctx, `SELECT effective_from,effective_until FROM ext_document_annotations WHERE tenant_id=? AND knowledge_base_id=? AND document_id=?`, args(scope, doc)...).Scan(&from, &until)
	if e != nil && !errors.Is(e, sql.ErrNoRows) {
		return e
	}
	if effective, _ := store.EffectiveAt(s.now(), from, until); !effective {
		return ErrConflict
	}
	return nil
}

func (s *Service) currentEnabled(ctx context.Context, tx *sql.Tx, scope provenance.Scope, doc, version string) error {
	if err := s.currentVersionEvidence(ctx, tx, scope, doc, version); err != nil {
		return err
	}
	var enabled sql.NullString
	err := tx.QueryRowContext(ctx, `SELECT json_extract(snapshot_json,'$.header_fields.enable_status')
		FROM ext_source_snapshot_evidence WHERE tenant_id=? AND knowledge_base_id=? AND version_node_id=?`,
		args(scope, version)...).Scan(&enabled)
	if errors.Is(err, sql.ErrNoRows) || err == nil && !enabled.Valid {
		return ErrUnavailable
	}
	if err != nil {
		return err
	}
	// The native snapshot hashes the JSON string, including quotes.
	if enabled.String != textHash(`"enabled"`) {
		return ErrConflict
	}
	return nil
}

// Execute dispatches writes without touching native documents or search indexes.
func (s *Service) Execute(ctx context.Context, c Command) (Result, error) {
	switch c.Action {
	case "family_create", "family_member_add", "family_member_remove":
		return s.family(ctx, c)
	case "family_head_recover":
		return s.recoverFamilyHead(ctx, c)
	case "family_replace_create", "family_replace_approve", "family_replace_cancel":
		return s.replaceFamilyMember(ctx, c)
	case "family_replace_history":
		return s.applyFamilyReplacementHistory(ctx, c)
	case "family_backfill_apply":
		return s.applyLegacyFamily(ctx, c)
	case "family_suggestion_policy_set":
		return s.setFamilySuggestionPolicy(ctx, c)
	case "family_suggestion_scan":
		return s.queueFamilySuggestionScan(ctx, c)
	case "family_suggestion_ignore", "family_suggestion_confirm":
		return s.reviewFamilySuggestion(ctx, c)
	case "relation_declare", "relation_confirm", "relation_revoke":
		return s.relation(ctx, c)
	case "feedback_create", "feedback_capture", "feedback_approve", "feedback_record_only", "feedback_needs_evidence", "feedback_reject", "feedback_quarantine", "feedback_activate", "feedback_withdraw", "feedback_sample_review", "feedback_topic_recover", "guidance_seed", "guidance_quarantine_topic":
		return s.feedback(ctx, c)
	case "revision_create", "revision_create_binary", "revision_approve", "revision_reject":
		return s.revision(ctx, c)
	case "revision_bind_binary":
		return s.bindBinaryRevision(ctx, c)
	case "split_create", "split_approve", "split_reject":
		return s.split(ctx, c)
	case "repeal_create", "repeal_approve", "repeal_cancel":
		return s.repeal(ctx, c)
	case "clause_create", "clause_confirm", "clause_resolve", "clause_revoke":
		return s.clause(ctx, c)
	default:
		return Result{}, ErrInvalid
	}
}
