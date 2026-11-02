package extensionbridge

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// SourceCopyBinding describes exact immutable bytes after native resource authorization.
type SourceCopyBinding struct {
	UserID         string `json:"user_id"`
	SessionID      string `json:"session_id"`
	ResourceID     string `json:"resource_id"`
	ContentVersion string `json:"content_version"`
	SHA256         string `json:"sha256"`
}

// SourceParent identifies an immutable previous copy, not a mutable file path.
type SourceParent struct {
	CopyID   string `json:"copy_id"`
	Revision int64  `json:"revision"`
}

// SourceCopyManifest contains source identifiers only; resolving is not a read permit.
type SourceCopyManifest struct {
	Scope        SourceScope       `json:"scope"`
	CopyID       string            `json:"copy_id"`
	Revision     int64             `json:"revision"`
	Status       string            `json:"status"`
	Binding      SourceCopyBinding `json:"binding"`
	EvidenceHash string            `json:"evidence_hash"`
	RunID        string            `json:"run_id"`
	Sources      []SourceRef       `json:"sources"`
	Parents      []SourceParent    `json:"parents"`
	CreatedAt    time.Time         `json:"created_at"`
}

// SourceCopyRead must be accompanied by freshly native-authorized explicit document IDs.
type SourceCopyRead struct {
	Scope    SourceScope       `json:"scope"`
	CopyID   string            `json:"copy_id"`
	Revision int64             `json:"revision"`
	Binding  SourceCopyBinding `json:"binding"`
}

// SourceProducer reserves an actual write operation, never a directory scan.
type SourceProducer struct {
	OperationID string            `json:"operation_id"`
	StepID      string            `json:"step_id"`
	CopyID      string            `json:"copy_id"`
	Path        string            `json:"path"`
	Binding     SourceCopyBinding `json:"binding"`
	Parents     []SourceParent    `json:"parents"`
}

// SourceProduced acknowledges exact actual read-back bytes without repeating the write.
type SourceProduced struct {
	OperationID string            `json:"operation_id"`
	ProducerID  string            `json:"producer_id"`
	StepID      string            `json:"step_id"`
	Binding     SourceCopyBinding `json:"binding"`
}
type sourceSave struct {
	Scope            SourceScope       `json:"scope"`
	OperationID      string            `json:"operation_id"`
	CopyID           string            `json:"copy_id"`
	ExpectedRevision int64             `json:"expected_revision"`
	Binding          SourceCopyBinding `json:"binding"`
	EvidenceID       string            `json:"evidence_id"`
}

// ResolveSourceCopy returns only an exact bound manifest, without granting access.
func ResolveSourceCopy(ctx context.Context, s SourceScope, b SourceCopyBinding) (SourceCopyManifest, error) {
	var m SourceCopyManifest
	if e := sourceCall(ctx, "/provenance/copies/resolve", sourceRequest{Scope: s, Binding: &b}, &m); e != nil {
		return m, e
	}
	if m.Scope != s || m.Binding != b || m.Status != "ready" || !sourceID(m.CopyID) || m.Revision <= 0 || len(m.Sources) == 0 || len(m.Sources) > 256 || !sourceDigest(m.EvidenceHash) {
		return SourceCopyManifest{}, ErrUnattributed
	}
	seen := map[string]bool{}
	for _, ref := range m.Sources {
		if !sourceID(ref.DocumentID) || !sourceID(ref.VersionID) || seen[ref.DocumentID] {
			return SourceCopyManifest{}, ErrUnattributed
		}
		seen[ref.DocumentID] = true
	}
	return m, nil
}

// CheckSourceCopy must follow native ACL resolution for every manifest source.
func CheckSourceCopy(ctx context.Context, m SourceCopyManifest, authorized []string) error {
	if len(m.Sources) == 0 || len(authorized) == 0 || len(authorized) > 256 {
		return ErrUnattributed
	}
	allowed := map[string]bool{}
	for _, doc := range authorized {
		if !sourceID(doc) || allowed[doc] {
			return ErrUnattributed
		}
		allowed[doc] = true
	}
	for _, ref := range m.Sources {
		if !allowed[ref.DocumentID] {
			return ErrUnattributed
		}
	}
	var d struct {
		Allowed bool   `json:"allowed"`
		Reason  string `json:"reason"`
	}
	read := SourceCopyRead{m.Scope, m.CopyID, m.Revision, m.Binding}
	if e := sourceCall(ctx, "/provenance/copies/check", sourceRequest{Scope: m.Scope, Read: &read, AuthorizedDocumentIDs: authorized}, &d); e != nil {
		return e
	}
	if !d.Allowed {
		return ErrUnattributed
	}
	return nil
}

// ReserveSourceProducer binds intended bytes before the original write. The
// returned ID is a receipt correlation, not a readable-copy permit.
func ReserveSourceProducer(ctx context.Context, q SourceProducer) (string, error) {
	h, ok := SourceRunFromContext(ctx)
	step, _ := ctx.Value(sourceStepKey{}).(string)
	if !ok || !sourceID(step) {
		return "", ErrUnattributed
	}
	q.StepID = step
	if q.OperationID == "" {
		q.OperationID = uuid.NewString()
	}
	var p struct {
		ProducerID string `json:"producer_id"`
	}
	if e := sourceCall(ctx, "/provenance/producers", sourceRequest{Scope: h.Scope, Handle: &h, Action: "reserve", Producer: &q}, &p); e != nil {
		return "", e
	}
	if !sourceID(p.ProducerID) {
		return "", ErrUnattributed
	}
	return p.ProducerID, nil
}

// CommitSourceProducer acknowledges only bytes actually read after a successful write.
func CommitSourceProducer(ctx context.Context, producerID string, b SourceCopyBinding, operationID string) error {
	h, ok := SourceRunFromContext(ctx)
	step, _ := ctx.Value(sourceStepKey{}).(string)
	if !ok || !sourceID(step) || !sourceID(operationID) {
		return ErrUnattributed
	}
	var result struct{}
	q := SourceProduced{operationID, producerID, step, b}
	return sourceCall(ctx, "/provenance/producers", sourceRequest{Scope: h.Scope, Handle: &h, Action: "commit", Produced: &q}, &result)
}
