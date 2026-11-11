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

// FamilyProjectionSetting reports a knowledge base's explicit cutover and unsettled tag work.
type FamilyProjectionSetting struct {
	Enabled  bool  `json:"enabled"`
	Revision int64 `json:"revision"`
	Pending  int64 `json:"pending"`
	Failed   int64 `json:"failed"`
}

// FamilyProjection reads the switch and the current tag projection backlog in one snapshot.
func (s *Service) FamilyProjection(ctx context.Context, scope provenance.Scope) (FamilyProjectionSetting, error) {
	var out FamilyProjectionSetting
	if s == nil || s.db == nil {
		return out, ErrUnavailable
	}
	if scope.TenantID == 0 || !validID(scope.KnowledgeBaseID) {
		return out, ErrInvalid
	}
	err := s.db.View(ctx, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `WITH scope(tenant_id,knowledge_base_id) AS (VALUES(?,?))
			SELECT COALESCE(p.enabled,0),COALESCE(p.revision,0),
				(SELECT count(*) FROM ext_projection_outbox o WHERE o.tenant_id=s.tenant_id AND o.knowledge_base_id=s.knowledge_base_id
				 AND o.projection_type='family_tag' AND o.status IN ('pending','processing')),
				(SELECT count(*) FROM ext_projection_outbox o WHERE o.tenant_id=s.tenant_id AND o.knowledge_base_id=s.knowledge_base_id
				 AND o.projection_type='family_tag' AND o.status='dead')
			FROM scope s LEFT JOIN ext_business_family_projection_scopes p USING(tenant_id,knowledge_base_id)`,
			args(scope)...).Scan(&out.Enabled, &out.Revision, &out.Pending, &out.Failed)
	})
	return out, err
}

func (s *Service) setFamilyProjection(ctx context.Context, c Command) (Result, error) {
	if c.ID != "" || c.Payload.ProjectionEnabled == nil || !c.Payload.SecondConfirmation {
		return Result{}, ErrInvalid
	}
	enabled := *c.Payload.ProjectionEnabled
	return s.transact(ctx, c, func(tx *sql.Tx) (Result, error) {
		var previous int64
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE((SELECT revision FROM ext_business_family_projection_scopes
			WHERE tenant_id=? AND knowledge_base_id=?),0)`, args(c.Scope)...).Scan(&previous); err != nil {
			return Result{}, err
		}
		if previous != c.ExpectedRevision {
			return Result{}, ErrConflict
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO ext_business_family_projection_scopes VALUES(?,?,?,?)
			ON CONFLICT(tenant_id,knowledge_base_id) DO UPDATE SET enabled=excluded.enabled,revision=excluded.revision`,
			args(c.Scope, enabled, previous+1)...); err != nil {
			return Result{}, err
		}
		docs, err := familyProjectionDocuments(ctx, tx, c.Scope)
		if err != nil {
			return Result{}, err
		}
		if err := enqueueFamilyProjection(ctx, tx, c, s.now().UTC(), docs); err != nil {
			return Result{}, err
		}
		status := "disabled"
		if enabled {
			status = "enabled"
		}
		return Result{ID: c.Scope.KnowledgeBaseID, Revision: previous + 1, Status: status}, nil
	})
}

func familyProjectionDocuments(ctx context.Context, tx *sql.Tx, scope provenance.Scope) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `WITH scope(tenant_id,knowledge_base_id) AS (VALUES(?,?))
		SELECT document_id FROM ext_business_document_family_members JOIN scope USING(tenant_id,knowledge_base_id)
		UNION SELECT document_id FROM ext_business_family_member_history JOIN scope USING(tenant_id,knowledge_base_id)
		UNION SELECT aggregate_id FROM ext_projection_outbox JOIN scope USING(tenant_id,knowledge_base_id)
			WHERE projection_type='family_tag' AND aggregate_type='document' AND causation_type=?`,
		args(scope, taggov.BusinessFamilyCausation)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var docs []string
	for rows.Next() {
		var doc string
		if err := rows.Scan(&doc); err != nil {
			return nil, err
		}
		docs = append(docs, doc)
	}
	return docs, rows.Err()
}

func enqueueFamilyProjectionIfEnabled(ctx context.Context, tx *sql.Tx, c Command, now time.Time, docs []string) error {
	var enabled bool
	err := tx.QueryRowContext(ctx, `SELECT COALESCE((SELECT enabled FROM ext_business_family_projection_scopes
		WHERE tenant_id=? AND knowledge_base_id=?),0)`, args(c.Scope)...).Scan(&enabled)
	if err != nil || !enabled {
		return err
	}
	return enqueueFamilyProjection(ctx, tx, c, now, docs)
}

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
