package provenance

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
)

// Save appends an immutable manifest revision, references, head, receipt and audit in one transaction.
// Evidence is resolved outside the writer, then all persisted versions/state are rechecked inside it.
func (l *Ledger) Save(ctx context.Context, r SaveRequest) (result Receipt, err error) {
	if !validScope(r.Scope) || !id(r.OperationID) || !id(r.CopyID) || !validBinding(r.Binding) || r.ExpectedRevision < 0 || r.EvidenceID != "" && !id(r.EvidenceID) {
		return result, ErrInvalid
	}
	hash := payloadHash("copy", r)
	if previous, exists, e := l.replay(ctx, r.Scope, r.OperationID, "copy", hash); e != nil || exists {
		return previous, e
	}
	m := Manifest{Scope: r.Scope, CopyID: r.CopyID, Revision: r.ExpectedRevision + 1, Status: "pending", Binding: r.Binding, Sources: []SourceRef{}, Parents: []ParentRef{}, CreatedAt: l.now().UTC()}
	if r.EvidenceID != "" {
		if l.authority == nil {
			return result, ErrEvidence
		}
		p, e := l.authority.SealedCopy(ctx, r.Scope, r.CopyID, r.Binding, r.EvidenceID)
		if e != nil || p.Scope != r.Scope || p.CopyID != r.CopyID || p.Binding != r.Binding || p.EvidenceID != r.EvidenceID || !digest(p.EvidenceHash) || !id(p.RunID) || len(p.Sources) > 256 || len(p.Parents) > 16 {
			return result, ErrEvidence
		}
		m.Status = "ready"
		m.EvidenceHash = p.EvidenceHash
		m.RunID = p.RunID
		m.Sources, e = sourceUnion(p.Sources)
		if e != nil {
			return result, e
		}
		seen := map[ParentRef]bool{}
		for _, parent := range p.Parents {
			if !id(parent.CopyID) || parent.CopyID == r.CopyID || parent.Revision <= 0 || seen[parent] {
				return result, ErrEvidence
			}
			seen[parent] = true
			m.Parents = append(m.Parents, parent)
		}
		sort.Slice(m.Parents, func(i, j int) bool {
			if m.Parents[i].CopyID == m.Parents[j].CopyID {
				return m.Parents[i].Revision < m.Parents[j].Revision
			}
			return m.Parents[i].CopyID < m.Parents[j].CopyID
		})
	}
	var prepared []SourceRef
	if m.Status == "ready" {
		var e error
		prepared, e = l.preparePermission(ctx, m)
		if e != nil {
			return Receipt{}, ErrEvidence
		}
	}
	err = l.db.Write(ctx, func(tx *sql.Tx) error {
		previous, exists, e := lookupReceipt(ctx, tx, r.Scope, r.OperationID, "copy", hash)
		if e != nil || exists {
			result = previous
			return e
		}
		old, e := loadManifest(ctx, tx, r.Scope, r.CopyID, 0)
		if e != nil && !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		if old.Revision != r.ExpectedRevision {
			return ErrConflict
		}
		if old.Revision > 0 && (old.Binding.UserID != r.Binding.UserID || old.Binding.SessionID != r.Binding.SessionID || old.Binding.ResourceID != r.Binding.ResourceID) {
			return ErrConflict
		}
		if old.Status == "ready" && m.Status != "ready" {
			return ErrConflict
		}
		for _, parent := range m.Parents {
			pm, e := loadManifest(ctx, tx, r.Scope, parent.CopyID, 0)
			if e != nil || pm.Revision != parent.Revision || pm.Status != "ready" || pm.Binding.UserID != m.Binding.UserID || pm.Binding.SessionID != m.Binding.SessionID {
				return ErrEvidence
			}
			if reason, e := l.checkManifest(ctx, tx, pm); e != nil || reason != "" {
				return ErrEvidence
			}
			m.Sources, e = sourceUnion(m.Sources, pm.Sources)
			if e != nil {
				return e
			}
		}
		if m.Status == "ready" {
			if len(m.Sources) == 0 {
				return ErrEvidence
			}
			if e := l.verifyPermissionTx(ctx, tx, m.Scope, m.Sources, prepared); e != nil {
				return ErrEvidence
			}
			for _, source := range m.Sources {
				reason, e := l.sourceReason(ctx, tx, r.Scope, source)
				if e != nil {
					return e
				}
				if reason != "" {
					return ErrEvidence
				}
			}
		}
		refs := map[SourceRef]bool{}
		for _, ref := range m.Sources {
			refs[ref] = true
		}
		for _, ref := range old.Sources {
			if !refs[ref] {
				return ErrConflict
			}
		}
		body, e := json.Marshal(m)
		if e != nil || len(body) > 128<<10 {
			return ErrInvalid
		}
		if _, e = tx.ExecContext(ctx, "INSERT INTO ext_provenance_manifests VALUES(?,?,?,?,?,?)", args(r.Scope, r.CopyID, m.Revision, m.Status, string(body))...); e != nil {
			return e
		}
		for _, ref := range m.Sources {
			if _, e = tx.ExecContext(ctx, "INSERT INTO ext_provenance_manifest_sources VALUES(?,?,?,?,?,?)", args(r.Scope, r.CopyID, m.Revision, ref.DocumentID, ref.VersionID)...); e != nil {
				return e
			}
		}
		for _, parent := range m.Parents {
			if _, e = tx.ExecContext(ctx, "INSERT INTO ext_provenance_manifest_parents VALUES(?,?,?,?,?,?)", args(r.Scope, r.CopyID, m.Revision, parent.CopyID, parent.Revision)...); e != nil {
				return e
			}
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO ext_provenance_copy_heads VALUES(?,?,?,?) ON CONFLICT(tenant_id,knowledge_base_id,copy_id) DO UPDATE SET revision=excluded.revision`, args(r.Scope, r.CopyID, m.Revision)...); e != nil {
			return e
		}
		// Validate the prospective head too: updating an ancestor can otherwise
		// make this very revision depend on a superseded ancestor revision.
		if m.Status == "ready" {
			if reason, e := l.checkManifest(ctx, tx, m); e != nil || reason != "" {
				return ErrEvidence
			}
		}
		result = Receipt{Kind: "copy", ID: r.CopyID, Revision: m.Revision, Status: m.Status}
		return l.receipt(ctx, tx, r.Scope, r.OperationID, hash, result)
	})
	if err != nil {
		return Receipt{}, err
	}
	return
}
