package lineage

import (
	"context"
	"database/sql"

	"github.com/ybochuan/LanRAG-Next/extensions/provenance"
)

// SealedCopyReader must certify complete actual run inputs; T1/T2 install none.
type SealedCopyReader interface {
	SealedCopy(context.Context, provenance.Scope, string, provenance.Binding, string) (provenance.CopyProof, error)
}

// Authority binds provenance to current observed native state. No browser/model claims are promoted.
type Authority struct {
	registry *Registry
	sealed   SealedCopyReader
}

// NewAuthority requires a real configured reader for source authority; nil sealed keeps copies denied.
func NewAuthority(r *Registry, sealed SealedCopyReader) *Authority {
	return &Authority{registry: r, sealed: sealed}
}

// CurrentSource recollects current native evidence; an old/fabricated evidence ID cannot be reused.
func (a *Authority) CurrentSource(ctx context.Context, s provenance.Scope, doc, evidence string) (provenance.SourceProof, error) {
	if a == nil || a.registry == nil {
		return provenance.SourceProof{}, ErrEvidence
	}
	v, e := a.registry.Refresh(ctx, s, doc)
	if e != nil || v.ID != evidence {
		return provenance.SourceProof{}, ErrEvidence
	}
	return provenance.SourceProof{Scope: s, DocumentID: doc, VersionID: v.ID, ContentHash: v.RootHash, ObservationSequence: v.Sequence, LifecycleEventID: v.LifecycleEventID, EvidenceID: v.ID, EvidenceHash: v.EvidenceHash}, nil
}

// SealedCopy never treats document roots as proof of complete model-run provenance.
func (a *Authority) SealedCopy(ctx context.Context, s provenance.Scope, copy string, b provenance.Binding, evidence string) (provenance.CopyProof, error) {
	if a == nil || a.sealed == nil {
		return provenance.CopyProof{}, ErrEvidence
	}
	p, e := a.sealed.SealedCopy(ctx, s, copy, b, evidence)
	if e != nil || a.VerifyCurrent(ctx, s, p.Sources) != nil {
		return provenance.CopyProof{}, ErrEvidence
	}
	return p, nil
}

// VerifyCurrent fetches before every permission decision; there is no allow cache.
func (a *Authority) VerifyCurrent(ctx context.Context, s provenance.Scope, refs []provenance.SourceRef) error {
	if a == nil || a.registry == nil || len(refs) == 0 || len(refs) > 256 {
		return ErrEvidence
	}
	for _, ref := range refs {
		v, e := a.registry.Refresh(ctx, s, ref.DocumentID)
		if e != nil || v.ID != ref.VersionID {
			return ErrEvidence
		}
	}
	return nil
}

// VerifyCurrentTx closes the local refresh/view race and rejects old unverified schema6 evidence.
func (a *Authority) VerifyCurrentTx(ctx context.Context, tx *sql.Tx, s provenance.Scope, refs []provenance.SourceRef) error {
	if len(refs) == 0 {
		return ErrEvidence
	}
	for _, ref := range refs {
		v, e := currentVersion(ctx, tx, s, ref.DocumentID)
		if e != nil || v.ID != ref.VersionID {
			return ErrEvidence
		}
	}
	return nil
}
