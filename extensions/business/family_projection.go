package business

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/outbox"
	"github.com/ybochuan/LanRAG-Next/extensions/provenance"
	"github.com/ybochuan/LanRAG-Next/extensions/taggov"
)

func enqueueFamilyProjection(ctx context.Context, tx *sql.Tx, c Command, now time.Time, docs []string) error {
	cause := digest(args(c.Scope, c.OperationID))
	payload, err := json.Marshal(map[string]string{"operation_id": c.OperationID})
	if err != nil {
		return err
	}
	for _, doc := range docs {
		if err := outbox.EnqueueTx(ctx, tx, outbox.Item{ID: "family-projection:" + cause + ":" + doc,
			TenantID: c.Scope.TenantID, KnowledgeBaseID: c.Scope.KnowledgeBaseID, ProjectionType: taggov.FamilyProjectionType,
			AggregateType: "document", AggregateID: doc, CausationType: taggov.BusinessFamilyCausation,
			CausationID: cause, Payload: payload, CreatedAt: now}); err != nil {
			return err
		}
	}
	return nil
}

func requireLegacyPublisherOutsideFamilyHead(ctx context.Context, tx *sql.Tx, scope provenance.Scope, doc string) error {
	var conflicts bool
	err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM ext_business_family_published_heads h
		WHERE h.tenant_id=? AND h.knowledge_base_id=? AND h.document_id=?)`, args(scope, doc)...).Scan(&conflicts)
	if err != nil {
		return err
	}
	if conflicts {
		return ErrConflict
	}
	return nil
}
