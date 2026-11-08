package lineage

import (
	"context"
	"database/sql"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/provenance"
)

// renditionGraph reports verified bindings for the latest observed versions.
// It does not fetch originals or expose result bytes.
func (r *Registry) renditionGraph(ctx context.Context, s provenance.Scope, out GraphSnapshot) (GraphSnapshot, error) {
	type candidate struct {
		edge                GraphEdge
		source              GraphNode
		boundAt, observedAt string
	}
	err := r.db.View(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT e.edge_id,e.from_node_id,e.to_node_id,e.edge_type,e.created_at,
			v.document_id,v.family_id,v.observation_sequence,v.created_at
			FROM ext_rendition_edges e
			JOIN ext_parser_rendition_bindings b ON b.artifact_node_id=e.from_node_id
			JOIN ext_parser_artifacts a ON a.identity_key=b.identity_key AND a.sha256=b.artifact_sha256
			JOIN ext_document_version v ON v.tenant_id=e.tenant_id AND v.knowledge_base_id=e.knowledge_base_id
			AND v.version_node_id=e.to_node_id AND v.document_id=b.document_id
			JOIN ext_document_version_family f ON f.tenant_id=v.tenant_id AND f.knowledge_base_id=v.knowledge_base_id
			AND f.document_id=v.document_id AND f.current_version_node_id=v.version_node_id
			WHERE e.tenant_id=? AND e.knowledge_base_id=? AND (?='' OR e.from_node_id=? OR e.to_node_id=?)
			ORDER BY e.created_at DESC,e.edge_id LIMIT 129`, scopeArgs(s, out.RootID, out.RootID, out.RootID)...)
		if err != nil {
			return err
		}
		var candidates []candidate
		for rows.Next() {
			var c candidate
			if err := rows.Scan(&c.edge.ID, &c.edge.From, &c.edge.To, &c.edge.Type, &c.boundAt,
				&c.source.DocumentID, &c.source.FamilyID, &c.source.Sequence, &c.observedAt); err != nil {
				rows.Close()
				return err
			}
			candidates = append(candidates, c)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		if len(candidates) > 128 {
			out.Truncated = true
			candidates = candidates[:128]
		}
		current := map[string]bool{}
		for _, c := range candidates {
			allowed, checked := current[c.edge.To]
			if !checked {
				_, err := renditionGovernance(ctx, tx, s, c.source.DocumentID, c.edge.To)
				allowed = err == nil
				current[c.edge.To] = allowed
				if allowed {
					created, err := time.Parse(time.RFC3339Nano, c.observedAt)
					if err != nil {
						return ErrUnavailable
					}
					c.source.ID, c.source.Type, c.source.State = c.edge.To, "document_version", "active"
					c.source.Current, c.source.CreatedAt = true, created.UTC()
					out.Nodes = append(out.Nodes, c.source)
				}
			}
			if !allowed {
				continue
			}
			bound, err := time.Parse(time.RFC3339Nano, c.boundAt)
			if err != nil {
				return ErrUnavailable
			}
			out.Nodes = append(out.Nodes, GraphNode{ID: c.edge.From, Type: "parser_artifact", CreatedAt: bound.UTC(), Title: "MinerU 结构化产物"})
			out.Edges = append(out.Edges, c.edge)
		}
		return nil
	})
	return out, err
}
