package lineage

import (
	"context"
	"database/sql"
	"errors"

	"github.com/ybochuan/LanRAG-Next/extensions/provenance"
)

// ErrTraversalLimit refuses unbounded or partial ancestry results.
var ErrTraversalLimit = errors.New("lineage traversal limit exceeded")

// Ancestor is a reachable node along the selected edge semantics, never document content.
type Ancestor struct {
	NodeID   string `json:"node_id"`
	EdgeType string `json:"edge_type"`
	Depth    int    `json:"depth"`
}

func requireNode(ctx context.Context, tx *sql.Tx, s provenance.Scope, id string) error {
	var one int
	if e := tx.QueryRowContext(ctx, "SELECT 1 FROM ext_lineage_node WHERE tenant_id=? AND knowledge_base_id=? AND node_id=?", scopeArgs(s, id)...).Scan(&one); e != nil {
		return ErrEvidence
	}
	return nil
}

func traverse(ctx context.Context, tx *sql.Tx, s provenance.Scope, id, mode string) ([]Ancestor, error) {
	condition := "edge_type='derived_from'"
	if mode == "version-ancestry" {
		condition = "edge_type IN ('supersedes','merged_into')"
	} else if mode != "provenance" {
		return nil, ErrEvidence
	}
	if e := requireNode(ctx, tx, s, id); e != nil {
		return nil, e
	}
	seen := map[string]bool{id: true}
	queue := []Ancestor{{NodeID: id}}
	out := []Ancestor{}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		rows, e := tx.QueryContext(ctx, "SELECT to_node_id,edge_type FROM ext_lineage_edge WHERE tenant_id=? AND knowledge_base_id=? AND from_node_id=? AND "+condition+" ORDER BY to_node_id,edge_type LIMIT 257", scopeArgs(s, current.NodeID)...)
		if e != nil {
			return nil, e
		}
		next := []Ancestor{}
		count := 0
		for rows.Next() {
			count++
			if count > 256 {
				rows.Close()
				return nil, ErrTraversalLimit
			}
			var target, edge string
			if e = rows.Scan(&target, &edge); e != nil {
				rows.Close()
				return nil, e
			}
			if !seen[target] {
				if current.Depth >= 10 || len(seen) >= 256 {
					rows.Close()
					return nil, ErrTraversalLimit
				}
				seen[target] = true
				next = append(next, Ancestor{target, edge, current.Depth + 1})
			}
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return nil, e
		}
		out = append(out, next...)
		queue = append(queue, next...)
	}
	return out, nil
}

// Query keeps /provenance (derived_from) separate from /version-ancestry
// (supersedes/merged_into). Native access must precede any future HTTP facade.
func (r *Registry) Query(ctx context.Context, s provenance.Scope, node, mode string) ([]Ancestor, error) {
	if r == nil || r.db == nil || s.TenantID == 0 || !validID(s.KnowledgeBaseID) || !validID(node) {
		return nil, ErrEvidence
	}
	var out []Ancestor
	e := r.db.View(ctx, func(tx *sql.Tx) error { var e error; out, e = traverse(ctx, tx, s, node, mode); return e })
	if e != nil {
		return nil, e
	}
	return out, nil
}
