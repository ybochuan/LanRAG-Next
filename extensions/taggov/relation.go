package taggov

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
)

// RelationProjectionType identifies a business relation tag intent.
const RelationProjectionType = "relation_tag"

// RelationCausation binds relation tag work to a durable relation event.
const RelationCausation = "relation_event"

const relationNamespace = "gov:relation:"

func (p *Projector) projectRelation(ctx context.Context, scope Scope) error {
	var projected []string
	err := p.writer.ApplyResolved(ctx, scope, relationNamespace, func(ctx context.Context) ([]Tag, error) {
		types, err := p.relationTypes(ctx, scope)
		if err != nil {
			return nil, err
		}
		projected = append([]string(nil), types...)
		tags := make([]Tag, 0, len(types))
		for _, edgeType := range types {
			tag, err := p.mapping.Resolve(ctx, scope, relationNamespace+edgeType)
			if err != nil {
				return nil, err
			}
			tags = append(tags, tag)
		}
		return tags, nil
	})
	if err != nil {
		return err
	}
	current, err := p.relationTypes(ctx, scope)
	if err != nil {
		return err
	}
	if !equalStrings(projected, current) {
		return errors.New("relations changed during projection; retry required")
	}
	return nil
}

func (p *Projector) relationTypes(ctx context.Context, scope Scope) ([]string, error) {
	var types []string
	err := p.db.View(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT DISTINCT edge_type
			FROM ext_document_relations
			WHERE tenant_id=? AND status='active' AND (
				(knowledge_base_id=? AND document_id=?) OR
				(target_knowledge_base_id=? AND target_document_id=?))
			ORDER BY edge_type`,
			strconv.FormatUint(scope.TenantID, 10), scope.KnowledgeBaseID, scope.DocumentID,
			scope.KnowledgeBaseID, scope.DocumentID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var edgeType string
			if err := rows.Scan(&edgeType); err != nil {
				return err
			}
			types = append(types, edgeType)
		}
		return rows.Err()
	})
	return types, err
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
