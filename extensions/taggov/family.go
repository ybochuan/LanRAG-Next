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

// BusinessFamilyCausation binds membership and cutover tag work to a durable business receipt.
const BusinessFamilyCausation = "business_operation"

const familyNamespace = "gov:family:"

var errFamilyProjectionUnmanaged = errors.New("document has no business family projection")

func (p *Projector) projectFamily(ctx context.Context, scope Scope) error {
	var projected string
	err := p.writer.ApplyResolved(ctx, scope, familyNamespace, func(ctx context.Context) ([]Tag, error) {
		var err error
		projected, err = p.publishedFamily(ctx, scope)
		if err != nil {
			return nil, err
		}
		if projected == "" {
			return nil, nil
		}
		tag, err := p.mapping.resolve(ctx, scope, familyNamespace+projected, true)
		if err != nil {
			return nil, err
		}
		return []Tag{tag}, nil
	})
	if errors.Is(err, errFamilyProjectionUnmanaged) {
		return nil
	}
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
		tenant := strconv.FormatUint(scope.TenantID, 10)
		var cutover sql.NullBool
		var managed, removed bool
		err := tx.QueryRowContext(ctx, `WITH scope(tenant_id,knowledge_base_id,document_id) AS (VALUES(?,?,?))
			SELECT p.enabled, EXISTS(SELECT 1 FROM ext_projection_outbox o
				WHERE o.tenant_id=s.tenant_id AND o.knowledge_base_id=s.knowledge_base_id AND o.aggregate_id=s.document_id
				AND o.projection_type='family_tag' AND o.aggregate_type='document' AND o.causation_type=?),
			EXISTS(SELECT 1 FROM ext_business_family_member_history m
				WHERE m.tenant_id=s.tenant_id AND m.knowledge_base_id=s.knowledge_base_id
				AND m.document_id=s.document_id AND m.status='removed')
			FROM scope s LEFT JOIN ext_business_family_projection_scopes p USING(tenant_id,knowledge_base_id)`,
			tenant, scope.KnowledgeBaseID, scope.DocumentID, BusinessFamilyCausation).Scan(&cutover, &managed, &removed)
		if err != nil {
			return err
		}
		if cutover.Bool {
			if err := tx.QueryRowContext(ctx, `SELECT COALESCE((SELECT business_family_id FROM ext_business_document_family_members
				WHERE tenant_id=? AND knowledge_base_id=? AND document_id=?),'')`, tenant, scope.KnowledgeBaseID, scope.DocumentID).Scan(&family); err != nil {
				return err
			}
			if family == "" && !(managed && removed) {
				return errFamilyProjectionUnmanaged
			}
			return nil
		}
		// The published revision chain, not the per-document observation family
		// or a historical Outbox payload, determines the projected family.
		rows, err := tx.QueryContext(ctx, `SELECT DISTINCT r.family_id FROM ext_revision_proposals r
WHERE r.tenant_id=? AND r.knowledge_base_id=? AND r.status IN ('published','superseded')
AND (r.target_document_id=? OR r.source_document_id=? AND NOT EXISTS (
	SELECT 1 FROM ext_revision_split_parts p WHERE p.tenant_id=r.tenant_id
	AND p.knowledge_base_id=r.knowledge_base_id AND p.proposal_id=r.proposal_id
)) LIMIT 2`, tenant, scope.KnowledgeBaseID, scope.DocumentID, scope.DocumentID)
		if err != nil {
			return err
		}
		defer rows.Close()
		if rows.Next() {
			if err := rows.Scan(&family); err != nil {
				return err
			}
		}
		if rows.Next() || (!weknora.ValidID(family) && !(cutover.Valid && managed && family == "")) {
			return errors.New("published document family missing or ambiguous")
		}
		return rows.Err()
	})
	return family, err
}
