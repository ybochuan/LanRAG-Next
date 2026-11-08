package taggov

import (
	"context"
	"database/sql"
	"errors"
	"strconv"

	"github.com/ybochuan/LanRAG-Next/extensions/weknora"
)

// FamilyProjectionType identifies the revision publisher's family tag intent.
const FamilyProjectionType = "family_tag"

const familyNamespace = "gov:family:"

func (p *Projector) projectFamily(ctx context.Context, scope Scope) error {
	var projected string
	err := p.writer.ApplyResolved(ctx, scope, familyNamespace, func(ctx context.Context) ([]Tag, error) {
		var err error
		projected, err = p.publishedFamily(ctx, scope)
		if err != nil {
			return nil, err
		}
		tag, err := p.mapping.resolve(ctx, scope, familyNamespace+projected, true)
		if err != nil {
			return nil, err
		}
		return []Tag{tag}, nil
	})
	if err != nil {
		return err
	}
	current, err := p.publishedFamily(ctx, scope)
	if err != nil {
		return err
	}
	if current != projected {
		return errors.New("published family changed during projection; retry required")
	}
	return nil
}

func (p *Projector) publishedFamily(ctx context.Context, scope Scope) (string, error) {
	var family string
	err := p.db.View(ctx, func(tx *sql.Tx) error {
		// The published revision chain, not the per-document observation family
		// or a historical Outbox payload, determines the projected family.
		rows, err := tx.QueryContext(ctx, `SELECT DISTINCT r.family_id FROM ext_revision_proposals r
WHERE r.tenant_id=? AND r.knowledge_base_id=? AND r.status IN ('published','superseded')
AND (r.target_document_id=? OR r.source_document_id=? AND NOT EXISTS (
	SELECT 1 FROM ext_revision_split_parts p WHERE p.tenant_id=r.tenant_id
	AND p.knowledge_base_id=r.knowledge_base_id AND p.proposal_id=r.proposal_id
)) LIMIT 2`, strconv.FormatUint(scope.TenantID, 10), scope.KnowledgeBaseID, scope.DocumentID, scope.DocumentID)
		if err != nil {
			return err
		}
		defer rows.Close()
		if rows.Next() {
			if err := rows.Scan(&family); err != nil {
				return err
			}
		}
		if rows.Next() || !weknora.ValidID(family) {
			return errors.New("published document family missing or ambiguous")
		}
		return rows.Err()
	})
	return family, err
}
