package provenance

import (
	"context"
	"database/sql"
	"errors"
)

// RegisterSource commits a verified current-version observation, its CAS head, receipt and audit together.
// A stale/unknown authoritative sequence cannot roll a head back to an earlier version.
func (l *Ledger) RegisterSource(ctx context.Context, r SourceRequest) (result Receipt, err error) {
	if !validScope(r.Scope) || !id(r.OperationID) || !id(r.DocumentID) || !id(r.EvidenceID) || r.ExpectedRevision < 0 {
		return result, ErrInvalid
	}
	hash := payloadHash("source", r)
	if previous, exists, e := l.replay(ctx, r.Scope, r.OperationID, "source", hash); e != nil || exists {
		return previous, e
	}
	if l.authority == nil {
		return result, ErrEvidence
	}
	proof, e := l.authority.CurrentSource(ctx, r.Scope, r.DocumentID, r.EvidenceID)
	if e != nil || proof.Scope != r.Scope || proof.DocumentID != r.DocumentID || proof.EvidenceID != r.EvidenceID || !id(proof.VersionID) || !digest(proof.ContentHash) || !digest(proof.EvidenceHash) || !id(proof.LifecycleEventID) || proof.ObservationSequence <= 0 {
		return result, ErrEvidence
	}
	err = l.db.Write(ctx, func(tx *sql.Tx) error {
		previous, exists, e := lookupReceipt(ctx, tx, r.Scope, r.OperationID, "source", hash)
		if e != nil || exists {
			result = previous
			return e
		}
		var revision, sequence int64
		var version string
		e = tx.QueryRowContext(ctx, "SELECT revision,version_id,native_sequence FROM ext_provenance_source_heads WHERE tenant_id=? AND knowledge_base_id=? AND document_id=?", args(r.Scope, r.DocumentID)...).Scan(&revision, &version, &sequence)
		if e != nil && !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		if revision != r.ExpectedRevision || proof.ObservationSequence < sequence || proof.ObservationSequence == sequence && version != proof.VersionID {
			return ErrConflict
		}
		// The immutable version anchor may predate later lifecycle transitions.
		// Current lifecycle is independently checked on every copy admission/read.
		var anchorCount int
		if e = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM ext_lifecycle_events WHERE tenant_id=? AND knowledge_base_id=? AND document_id=? AND event_id=?", args(r.Scope, r.DocumentID, proof.LifecycleEventID)...).Scan(&anchorCount); e != nil || anchorCount != 1 {
			return ErrEvidence
		}
		var content, evidence, anchor string
		var storedSequence int64
		e = tx.QueryRowContext(ctx, "SELECT content_hash,native_sequence,lifecycle_event_id,evidence_hash FROM ext_provenance_sources WHERE tenant_id=? AND knowledge_base_id=? AND document_id=? AND version_id=?", args(r.Scope, r.DocumentID, proof.VersionID)...).Scan(&content, &storedSequence, &anchor, &evidence)
		switch {
		case errors.Is(e, sql.ErrNoRows):
			_, e = tx.ExecContext(ctx, "INSERT INTO ext_provenance_sources VALUES(?,?,?,?,?,?,?,?)", args(r.Scope, r.DocumentID, proof.VersionID, proof.ContentHash, proof.ObservationSequence, proof.LifecycleEventID, proof.EvidenceHash)...)
			if e != nil {
				return e
			}
		case e != nil:
			return e
		case content != proof.ContentHash || storedSequence != proof.ObservationSequence || anchor != proof.LifecycleEventID || evidence != proof.EvidenceHash:
			return ErrConflict
		}
		revision++
		_, e = tx.ExecContext(ctx, `INSERT INTO ext_provenance_source_heads VALUES(?,?,?,?,?,?) ON CONFLICT(tenant_id,knowledge_base_id,document_id) DO UPDATE SET revision=excluded.revision,version_id=excluded.version_id,native_sequence=excluded.native_sequence`, args(r.Scope, r.DocumentID, revision, proof.VersionID, proof.ObservationSequence)...)
		if e != nil {
			return e
		}
		result = Receipt{Kind: "source", ID: r.DocumentID, Revision: revision, Status: "registered"}
		return l.receipt(ctx, tx, r.Scope, r.OperationID, hash, result)
	})
	if err != nil {
		return Receipt{}, err
	}
	return
}
