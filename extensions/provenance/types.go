// Package provenance owns synchronous copy-source facts, independently of the asynchronous lineage projection.
package provenance

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/store"
)

var (
	ErrInvalid     = errors.New("invalid provenance request")
	ErrConflict    = errors.New("provenance revision or identity conflict")
	ErrEvidence    = errors.New("trusted provenance evidence unavailable or invalid")
	ErrUnavailable = errors.New("provenance database unavailable")
)

// Scope restricts this stage to one tenant and KB; cross-scope sources are unsupported.
type Scope struct {
	TenantID        uint64 `json:"tenant_id"`
	KnowledgeBaseID string `json:"knowledge_base_id"`
}

// Binding is server-derived identity and exact immutable bytes, never a model-supplied ACL.
type Binding struct {
	UserID         string `json:"user_id"`
	SessionID      string `json:"session_id"`
	ResourceID     string `json:"resource_id"`
	ContentVersion string `json:"content_version"`
	SHA256         string `json:"sha256"`
}

// SourceRef identifies a trusted immutable source version within the surrounding Scope.
type SourceRef struct {
	DocumentID string `json:"document_id"`
	VersionID  string `json:"version_id"`
}

// ParentRef binds a previous immutable copy revision, not a mutable file path.
type ParentRef struct {
	CopyID   string `json:"copy_id"`
	Revision int64  `json:"revision"`
}

// SourceProof is attested by the configured current-version adapter. ObservationSequence
// orders extension observations, not native writes or unseen intermediate history.
type SourceProof struct {
	Scope               Scope  `json:"scope"`
	DocumentID          string `json:"document_id"`
	VersionID           string `json:"version_id"`
	ContentHash         string `json:"content_hash"`
	ObservationSequence int64  `json:"observation_sequence"`
	LifecycleEventID    string `json:"lifecycle_event_id"`
	EvidenceID          string `json:"evidence_id"`
	EvidenceHash        string `json:"evidence_hash"`
}

// CopyProof certifies a sealed complete input set. Callers cannot replace the trusted adapter with a boolean flag.
type CopyProof struct {
	Scope        Scope       `json:"scope"`
	CopyID       string      `json:"copy_id"`
	Binding      Binding     `json:"binding"`
	EvidenceID   string      `json:"evidence_id"`
	EvidenceHash string      `json:"evidence_hash"`
	RunID        string      `json:"run_id"`
	Sources      []SourceRef `json:"sources"`
	Parents      []ParentRef `json:"parents"`
}

// Authority is a server-only trust boundary implemented by trusted native adapters.
// It must attest current source versions and complete sealed run inputs for the exact scope and binding.
// Production copy proofs come from persisted Runs, never browser/model completeness claims.
type Authority interface {
	CurrentSource(context.Context, Scope, string, string) (SourceProof, error)
	SealedCopy(context.Context, Scope, string, Binding, string) (CopyProof, error)
}

// FreshAuthority revalidates current native evidence before a permit and its
// persisted authority head again inside the final view. Production lineage
// authority implements both; static offline test fixtures do not attest native data.
type FreshAuthority interface {
	VerifyCurrent(context.Context, Scope, []SourceRef) error
	VerifyCurrentTx(context.Context, *sql.Tx, Scope, []SourceRef) error
}

// SourceRequest uses CAS on the current-version head and an idempotent operation identity.
type SourceRequest struct {
	Scope            Scope  `json:"scope"`
	OperationID      string `json:"operation_id"`
	DocumentID       string `json:"document_id"`
	EvidenceID       string `json:"evidence_id"`
	ExpectedRevision int64  `json:"expected_revision"`
}

// SaveRequest without EvidenceID stores a pending draft; a nonempty ID requires a trusted sealed proof.
type SaveRequest struct {
	Scope            Scope   `json:"scope"`
	OperationID      string  `json:"operation_id"`
	CopyID           string  `json:"copy_id"`
	ExpectedRevision int64   `json:"expected_revision"`
	Binding          Binding `json:"binding"`
	EvidenceID       string  `json:"evidence_id"`
}

// Receipt is a historical mutation result, including on retry. It is NEVER a current read permit.
type Receipt struct {
	Kind     string `json:"kind"`
	ID       string `json:"id"`
	Revision int64  `json:"revision"`
	Status   string `json:"status"`
}

// Manifest stores an immutable full source union and content/owner binding for one copy revision.
type Manifest struct {
	Scope        Scope       `json:"scope"`
	CopyID       string      `json:"copy_id"`
	Revision     int64       `json:"revision"`
	Status       string      `json:"status"`
	Binding      Binding     `json:"binding"`
	EvidenceHash string      `json:"evidence_hash"`
	RunID        string      `json:"run_id"`
	Sources      []SourceRef `json:"sources"`
	Parents      []ParentRef `json:"parents"`
	CreatedAt    time.Time   `json:"created_at"`
}

// ReadRequest must follow native authorization and describe the exact bytes being exposed, including cached bytes.
type ReadRequest struct {
	Scope    Scope   `json:"scope"`
	CopyID   string  `json:"copy_id"`
	Revision int64   `json:"revision"`
	Binding  Binding `json:"binding"`
}

// Decision is deny by default. Reason contains no other owner's or source's identifiers.
type Decision struct {
	Allowed bool   `json:"allowed"`
	Reason  string `json:"reason"`
}

// Ledger shares the extension writer and never owns or closes the DB.
type Ledger struct {
	db        *store.DB
	authority Authority
	now       func() time.Time
}

// New configures an offline ledger. Nil authority can only create pending drafts.
func New(db *store.DB, authority Authority) *Ledger {
	return &Ledger{db: db, authority: authority, now: time.Now}
}
