package lineage

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/provenance"
)

// RegisterProjectionNode is an idempotent adapter for reconstructable node
// projections. Document versions can only be created from trusted snapshots.
func (r *Registry) RegisterProjectionNode(ctx context.Context, s provenance.Scope, id, kind, native string) error {
	if r == nil || r.db == nil || s.TenantID == 0 || !validID(s.KnowledgeBaseID) || !validID(id) || !validID(native) || (kind != "chunk" && kind != "parser_artifact" && kind != "chunk_revision") {
		return ErrEvidence
	}
	return r.db.Write(ctx, func(tx *sql.Tx) error {
		var tenant, kb, storedKind, storedNative string
		e := tx.QueryRowContext(ctx, "SELECT tenant_id,knowledge_base_id,node_type,native_id FROM ext_lineage_node WHERE node_id=?", id).Scan(&tenant, &kb, &storedKind, &storedNative)
		if e == nil {
			if tenant != strconv.FormatUint(s.TenantID, 10) || kb != s.KnowledgeBaseID || storedKind != kind || storedNative != native {
				return ErrConflict
			}
			return nil
		}
		if !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		_, e = tx.ExecContext(ctx, "INSERT INTO ext_lineage_node VALUES(?,?,?,?,?,?)", id, strconv.FormatUint(s.TenantID, 10), s.KnowledgeBaseID, kind, native, time.Now().UTC().Format(time.RFC3339Nano))
		return e
	})
}

// RecordDerived is for the asynchronous projection consumer, not an authority
// fact or admission decision. There is intentionally no generic critical-edge writer.
func (r *Registry) RecordDerived(ctx context.Context, s provenance.Scope, from, to string) error {
	if r == nil || r.db == nil || s.TenantID == 0 || !validID(s.KnowledgeBaseID) || !validID(from) || !validID(to) || from == to {
		return ErrEvidence
	}
	return r.db.Write(ctx, func(tx *sql.Tx) error {
		if e := requireNode(ctx, tx, s, from); e != nil {
			return e
		}
		parents, e := traverse(ctx, tx, s, to, "provenance")
		if e != nil {
			return e
		}
		for _, p := range parents {
			if p.NodeID == from {
				return ErrConflict
			}
		}
		id, e := newID()
		if e != nil {
			return e
		}
		_, e = tx.ExecContext(ctx, `INSERT INTO ext_lineage_edge VALUES(?,?,?,?,?,'derived_from','reconstructable',?) ON CONFLICT(tenant_id,knowledge_base_id,from_node_id,to_node_id,edge_type) DO NOTHING`, id, strconv.FormatUint(s.TenantID, 10), s.KnowledgeBaseID, from, to, time.Now().UTC().Format(time.RFC3339Nano))
		return e
	})
}
