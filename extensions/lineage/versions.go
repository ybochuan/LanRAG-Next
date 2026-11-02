package lineage

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/provenance"
	"github.com/ybochuan/LanRAG-Next/extensions/store"
)

// Version is an immutable observed state, not an inferred normative supersession.
type Version struct {
	ID               string `json:"id"`
	FamilyID         string `json:"family_id"`
	DocumentID       string `json:"document_id"`
	Sequence         int64  `json:"observation_sequence"`
	RootHash         string `json:"root_hash"`
	EvidenceHash     string `json:"evidence_hash"`
	LifecycleEventID string `json:"lifecycle_event_id"`
}

// Registry acquires and registers current native snapshots through the shared writer.
type Registry struct {
	db     *store.DB
	reader SnapshotReader
}

// NewRegistry does not own DB lifetime; nil readers can never register trusted versions.
func NewRegistry(db *store.DB, reader SnapshotReader) *Registry {
	return &Registry{db: db, reader: reader}
}
func scopeArgs(s provenance.Scope, tail ...any) []any {
	return append([]any{strconv.FormatUint(s.TenantID, 10), s.KnowledgeBaseID}, tail...)
}
func newID() (string, error) {
	b := make([]byte, 16)
	_, e := rand.Read(b)
	return hex.EncodeToString(b), e
}
func currentVersion(ctx context.Context, tx *sql.Tx, s provenance.Scope, doc string) (Version, error) {
	var v Version
	var body []byte
	e := tx.QueryRowContext(ctx, `SELECT v.version_node_id,v.family_id,v.document_id,v.observation_sequence,v.content_hash,e.evidence_hash,v.lifecycle_event_id,e.snapshot_json
 FROM ext_document_version_family f JOIN ext_document_version v ON v.tenant_id=f.tenant_id AND v.knowledge_base_id=f.knowledge_base_id AND v.family_id=f.family_id AND v.document_id=f.document_id AND v.version_node_id=f.current_version_node_id
 JOIN ext_source_snapshot_evidence e ON e.tenant_id=v.tenant_id AND e.knowledge_base_id=v.knowledge_base_id AND e.version_node_id=v.version_node_id
 WHERE f.tenant_id=? AND f.knowledge_base_id=? AND f.document_id=?`, scopeArgs(s, doc)...).Scan(&v.ID, &v.FamilyID, &v.DocumentID, &v.Sequence, &v.RootHash, &v.EvidenceHash, &v.LifecycleEventID, &body)
	if e == nil {
		var snapshot Snapshot
		if json.Unmarshal(body, &snapshot) != nil || snapshot.Validate(s, doc) != nil || snapshot.RootHash != v.RootHash || sha(body) != v.EvidenceHash {
			return Version{}, ErrEvidence
		}
	}
	return v, e
}

// Refresh holds BEGIN IMMEDIATE from acquisition through commit. A second process
// must acquire the same writer before collecting, so stale responses cannot retry a head update.
// The only remote work is a bounded read; its failure rolls back every local fact.
func (r *Registry) Refresh(ctx context.Context, s provenance.Scope, doc string) (Version, error) {
	if r == nil || r.db == nil || r.reader == nil || s.TenantID == 0 || !validID(s.KnowledgeBaseID) || !validID(doc) {
		return Version{}, ErrEvidence
	}
	var result Version
	err := r.db.Write(ctx, func(tx *sql.Tx) error {
		fetchCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
		defer cancel()
		snapshot, e := r.reader.Capture(fetchCtx, s, doc)
		if e != nil || fetchCtx.Err() != nil || snapshot.Validate(s, doc) != nil {
			return ErrEvidence
		}
		previous, e := currentVersion(ctx, tx, s, doc)
		if e != nil && !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		if e == nil && previous.RootHash == snapshot.RootHash {
			result = previous
			return nil
		}
		_, anchor, e := store.ReadLifecycleCurrentTx(ctx, tx, s.TenantID, s.KnowledgeBaseID, doc)
		if e != nil {
			return ErrEvidence
		}
		node, e := newID()
		if e != nil {
			return e
		}
		family := previous.FamilyID
		if family == "" {
			family, e = newID()
			if e != nil {
				return e
			}
		}
		body, e := json.Marshal(snapshot)
		if e != nil {
			return ErrEvidence
		}
		now := time.Now().UTC().Format(time.RFC3339Nano)
		sequence := previous.Sequence
		if previous.ID == "" {
			// Preserve the old bookkeeping fence without promoting old evidence.
			var legacy int64
			e = tx.QueryRowContext(ctx, "SELECT native_sequence FROM ext_provenance_source_heads WHERE tenant_id=? AND knowledge_base_id=? AND document_id=?", scopeArgs(s, doc)...).Scan(&legacy)
			if e != nil && !errors.Is(e, sql.ErrNoRows) {
				return e
			}
			if legacy > sequence {
				sequence = legacy
			}
		}
		if sequence == 1<<63-1 {
			return ErrConflict
		}
		result = Version{node, family, doc, sequence + 1, snapshot.RootHash, sha(body), anchor}
		if _, e = tx.ExecContext(ctx, "INSERT INTO ext_lineage_node VALUES(?,?,?,?,?,?)", node, strconv.FormatUint(s.TenantID, 10), s.KnowledgeBaseID, "document_version", doc, now); e != nil {
			return e
		}
		if previous.ID == "" {
			_, e = tx.ExecContext(ctx, "INSERT INTO ext_document_version_family VALUES(?,?,?,?,?,?,?,?,?,?)", family, strconv.FormatUint(s.TenantID, 10), s.KnowledgeBaseID, doc, "", node, node, result.Sequence, now, now)
		} else {
			var changed sql.Result
			changed, e = tx.ExecContext(ctx, "UPDATE ext_document_version_family SET current_version_node_id=?,observation_sequence=?,updated_at=? WHERE tenant_id=? AND knowledge_base_id=? AND document_id=? AND current_version_node_id=? AND observation_sequence=?", append([]any{node, result.Sequence, now}, scopeArgs(s, doc, previous.ID, previous.Sequence)...)...)
			if e == nil {
				n, x := changed.RowsAffected()
				if x != nil || n != 1 {
					return ErrConflict
				}
			}
		}
		if e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, "INSERT INTO ext_document_version VALUES(?,?,?,?,?,?,?,?,?,?,?)", node, strconv.FormatUint(s.TenantID, 10), s.KnowledgeBaseID, family, doc, result.RootHash, result.Sequence, anchor, "", Scheme, now); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, "INSERT INTO ext_source_snapshot_evidence VALUES(?,?,?,?,?)", scopeArgs(s, node, string(body), result.EvidenceHash)...); e != nil {
			return e
		}
		_, e = tx.ExecContext(ctx, "INSERT INTO ext_lineage_audit(tenant_id,knowledge_base_id,version_node_id,operation_id,payload_hash,occurred_at) VALUES(?,?,?,?,?,?)", scopeArgs(s, node, node, result.EvidenceHash, now)...)
		return e
	})
	if err != nil {
		if errors.Is(err, ErrEvidence) || errors.Is(err, ErrConflict) {
			return Version{}, err
		}
		return Version{}, ErrUnavailable
	}
	return result, nil
}
