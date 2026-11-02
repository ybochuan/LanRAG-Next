package provenance

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/dependencies"
	"github.com/ybochuan/LanRAG-Next/extensions/store"
)

func (l *Ledger) sourceReason(ctx context.Context, tx *sql.Tx, s Scope, ref SourceRef) (string, error) {
	var version, state string
	var from, until sql.NullString
	err := tx.QueryRowContext(ctx, `SELECT h.version_id,CASE WHEN `+store.LifecycleCurrentVerifiedSQL+` THEN c.state ELSE '' END,a.effective_from,a.effective_until
 FROM ext_provenance_source_heads h JOIN ext_lifecycle_current c ON c.tenant_id=h.tenant_id AND c.knowledge_base_id=h.knowledge_base_id AND c.document_id=h.document_id
 `+store.LifecycleEventJoinSQL+` LEFT JOIN ext_document_annotations a ON a.tenant_id=c.tenant_id AND a.knowledge_base_id=c.knowledge_base_id AND a.document_id=c.document_id
 WHERE h.tenant_id=? AND h.knowledge_base_id=? AND h.document_id=?`, args(s, ref.DocumentID)...).Scan(&version, &state, &from, &until)
	if errors.Is(err, sql.ErrNoRows) {
		return "source_unknown", nil
	}
	if err != nil {
		return "", err
	}
	if version != ref.VersionID {
		return "source_changed", nil
	}
	if state != "active" {
		return "source_inactive", nil
	}
	now := l.now().UTC()
	for _, deadline := range []struct {
		value sql.NullString
		start bool
	}{{from, true}, {until, false}} {
		if !deadline.value.Valid {
			continue
		}
		date, e := time.Parse(time.RFC3339Nano, deadline.value.String)
		if e != nil {
			return "source_unknown", nil
		}
		if deadline.start && now.Before(date) || !deadline.start && now.After(date) {
			return "source_not_effective", nil
		}
	}
	a, err := dependencies.LoadTx(ctx, tx, dependencies.Scope{TenantID: s.TenantID, KnowledgeBaseID: s.KnowledgeBaseID, DocumentID: ref.DocumentID})
	if err != nil {
		return "", err
	}
	if a.Invalid(now) {
		return "source_dependency_invalid", nil
	}
	return "", nil
}

// checkParents invalidates descendants when any ancestor's current manifest is
// corrected. Traversal is bounded and detects cycles even in malformed stored data.
func (l *Ledger) checkParents(ctx context.Context, tx *sql.Tx, m Manifest, seen map[ParentRef]uint8, depth int) (string, error) {
	key := ParentRef{m.CopyID, m.Revision}
	if seen[key] == 1 {
		return "lineage_cycle", nil
	}
	if seen[key] == 2 {
		return "", nil
	}
	if depth > 32 || len(seen) >= 128 || len(m.Parents) > 16 {
		return "lineage_limit", nil
	}
	seen[key] = 1
	rows, err := tx.QueryContext(ctx, "SELECT parent_copy_id,parent_revision FROM ext_provenance_manifest_parents WHERE tenant_id=? AND knowledge_base_id=? AND copy_id=? AND revision=? ORDER BY parent_copy_id,parent_revision", args(m.Scope, m.CopyID, m.Revision)...)
	if err != nil {
		return "", err
	}
	parents := []ParentRef{}
	for rows.Next() {
		var p ParentRef
		if err = rows.Scan(&p.CopyID, &p.Revision); err != nil {
			rows.Close()
			return "", err
		}
		parents = append(parents, p)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return "", err
	}
	if !reflect.DeepEqual(parents, m.Parents) {
		return "", ErrUnavailable
	}
	for _, p := range parents {
		parent, e := loadManifest(ctx, tx, m.Scope, p.CopyID, 0)
		if errors.Is(e, sql.ErrNoRows) {
			return "parent_unknown", nil
		}
		if e != nil {
			return "", e
		}
		if parent.Revision != p.Revision {
			return "parent_changed", nil
		}
		if parent.Status != "ready" || parent.Binding.UserID != m.Binding.UserID || parent.Binding.SessionID != m.Binding.SessionID {
			return "parent_unavailable", nil
		}
		reason, e := l.checkParents(ctx, tx, parent, seen, depth+1)
		if e != nil || reason != "" {
			return reason, e
		}
	}
	seen[key] = 2
	return "", nil
}

func (l *Ledger) checkManifest(ctx context.Context, tx *sql.Tx, m Manifest) (string, error) {
	if m.Status != "ready" {
		return "manifest_pending", nil
	}
	if len(m.Sources) == 0 || len(m.Sources) > 256 || !digest(m.EvidenceHash) || !id(m.RunID) {
		return "", ErrUnavailable
	}
	rows, e := tx.QueryContext(ctx, "SELECT document_id,version_id FROM ext_provenance_manifest_sources WHERE tenant_id=? AND knowledge_base_id=? AND copy_id=? AND revision=? ORDER BY document_id", args(m.Scope, m.CopyID, m.Revision)...)
	if e != nil {
		return "", e
	}
	refs := []SourceRef{}
	for rows.Next() {
		var ref SourceRef
		if e = rows.Scan(&ref.DocumentID, &ref.VersionID); e != nil {
			rows.Close()
			return "", e
		}
		refs = append(refs, ref)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return "", e
	}
	if !reflect.DeepEqual(refs, m.Sources) {
		return "", ErrUnavailable
	}
	for _, ref := range refs {
		reason, e := l.sourceReason(ctx, tx, m.Scope, ref)
		if e != nil || reason != "" {
			return reason, e
		}
	}
	return l.checkParents(ctx, tx, m, map[ParentRef]uint8{}, 0)
}

// CheckRead refreshes native evidence, then rechecks the entire transitive source
// union in one final read snapshot. No native ACL is inferred or expanded.
func (l *Ledger) CheckRead(ctx context.Context, r ReadRequest) (decision Decision, err error) {
	decision.Reason = "invalid_request"
	if !validScope(r.Scope) || !id(r.CopyID) || r.Revision <= 0 || !validBinding(r.Binding) {
		return decision, ErrInvalid
	}
	if l == nil || l.db == nil {
		decision.Reason = "unavailable"
		return decision, ErrUnavailable
	}
	var prepared []SourceRef
	if _, ok := l.authority.(FreshAuthority); ok {
		m, e := l.Snapshot(ctx, r.Scope, r.CopyID)
		if e != nil && !errors.Is(e, sql.ErrNoRows) {
			return Decision{Reason: "unavailable"}, ErrUnavailable
		}
		if e == nil && m.Status == "ready" && m.Revision == r.Revision && m.Binding == r.Binding {
			prepared, e = l.preparePermission(ctx, m)
			if e != nil {
				return Decision{Reason: "source_unverified"}, ErrUnavailable
			}
		}
	}
	err = l.db.View(ctx, func(tx *sql.Tx) error {
		m, e := loadManifest(ctx, tx, r.Scope, r.CopyID, 0)
		if errors.Is(e, sql.ErrNoRows) {
			decision.Reason = "not_found"
			return nil
		}
		if e != nil {
			return e
		}
		if m.Revision != r.Revision || m.Binding != r.Binding {
			decision.Reason = "binding_mismatch"
			return nil
		}
		reason, e := l.checkManifest(ctx, tx, m)
		if e != nil {
			return e
		}
		if reason != "" {
			decision.Reason = reason
			return nil
		}
		if e := l.verifyPermissionTx(ctx, tx, r.Scope, m.Sources, prepared); e != nil {
			return e
		}
		decision = Decision{Allowed: true, Reason: "active_sources"}
		return nil
	})
	if err != nil {
		decision = Decision{Reason: "unavailable"}
		return decision, ErrUnavailable
	}
	return
}
