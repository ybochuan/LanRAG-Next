package provenance

import (
	"context"
	"database/sql"

	"github.com/ybochuan/LanRAG-Next/extensions/dependencies"
)

// permissionRefsTx expands governance dependencies only for permission checks.
// These refs must never be stored as if their content entered the model.
func (l *Ledger) permissionRefsTx(ctx context.Context, tx *sql.Tx, s Scope, refs []SourceRef) ([]SourceRef, error) {
	expanded, e := sourceUnion(refs)
	if e != nil {
		return nil, e
	}
	for _, ref := range refs {
		a, e := dependencies.LoadTx(ctx, tx, dependencies.Scope{TenantID: s.TenantID, KnowledgeBaseID: s.KnowledgeBaseID, DocumentID: ref.DocumentID})
		if e != nil {
			return nil, e
		}
		if a.Invalid(l.now().UTC()) {
			return nil, ErrEvidence
		}
		parents := make([]SourceRef, 0, len(a.Sources))
		for _, p := range a.Sources {
			parents = append(parents, SourceRef{DocumentID: p.DocumentID, VersionID: p.VersionID})
		}
		expanded, e = sourceUnion(expanded, parents)
		if e != nil {
			return nil, e
		}
	}
	return expanded, nil
}

// preparePermission performs remote reads before a final extension transaction.
// Parents are immutable copy references; recheck their current heads at commit.
func (l *Ledger) preparePermission(ctx context.Context, m Manifest) ([]SourceRef, error) {
	var refs []SourceRef
	e := l.db.View(ctx, func(tx *sql.Tx) error {
		all := m.Sources
		for _, parent := range m.Parents {
			pm, e := loadManifest(ctx, tx, m.Scope, parent.CopyID, 0)
			if e != nil {
				return e
			}
			if pm.Revision != parent.Revision || pm.Status != "ready" || pm.Binding.UserID != m.Binding.UserID || pm.Binding.SessionID != m.Binding.SessionID {
				return ErrEvidence
			}
			all, e = sourceUnion(all, pm.Sources)
			if e != nil {
				return e
			}
		}
		var e error
		refs, e = l.permissionRefsTx(ctx, tx, m.Scope, all)
		return e
	})
	if e != nil {
		return nil, e
	}
	if fresh, ok := l.authority.(FreshAuthority); ok {
		if e = fresh.VerifyCurrent(ctx, m.Scope, refs); e != nil {
			return nil, e
		}
	}
	return refs, nil
}

func (l *Ledger) verifyPermissionTx(ctx context.Context, tx *sql.Tx, s Scope, refs, prepared []SourceRef) error {
	current, e := l.permissionRefsTx(ctx, tx, s, refs)
	if e != nil {
		return e
	}
	if fresh, ok := l.authority.(FreshAuthority); ok {
		verified := map[SourceRef]bool{}
		for _, ref := range prepared {
			verified[ref] = true
		}
		for _, ref := range current {
			if !verified[ref] {
				return ErrEvidence
			}
		}
		return fresh.VerifyCurrentTx(ctx, tx, s, current)
	}
	return nil
}
