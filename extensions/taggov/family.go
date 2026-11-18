package taggov

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
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
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE((SELECT business_family_id FROM ext_business_document_family_members
			WHERE tenant_id=? AND knowledge_base_id=? AND document_id=?),'')`, tenant, scope.KnowledgeBaseID, scope.DocumentID).Scan(&family); err != nil {
			return err
		}
		if family != "" {
			return nil
		}
		var removed bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM ext_business_family_member_history
			WHERE tenant_id=? AND knowledge_base_id=? AND document_id=? AND status='removed')`, tenant, scope.KnowledgeBaseID, scope.DocumentID).Scan(&removed); err != nil {
			return err
		}
		if !removed {
			return errFamilyProjectionUnmanaged
		}
		return nil
	})
	return family, err
}
