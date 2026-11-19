package business

import (
	"context"
	"database/sql"

	"github.com/ybochuan/LanRAG-Next/extensions/provenance"
	"github.com/ybochuan/LanRAG-Next/extensions/weknora"
)

func (s *Service) reviewFamilySuggestion(ctx context.Context, c Command) (Result, error) {
	if s == nil || s.db == nil {
		return Result{}, ErrUnavailable
	}
	if c.Scope.TenantID == 0 || !validID(c.Scope.KnowledgeBaseID) || !validID(c.ActorID) ||
		!validID(c.OperationID) || !validID(c.ID) || c.ExpectedRevision < 1 {
		return Result{}, ErrInvalid
	}
	if receipt, ok, err := s.replay(ctx, c); ok || err != nil {
		return receipt, err
	}
	var item FamilySuggestion
	err := s.db.View(ctx, func(tx *sql.Tx) error {
		var err error
		item, err = readSuggestion(ctx, tx, c.Scope, c.ID)
		return err
	})
	if err != nil {
		return s.replayAfterObservationError(ctx, c, err)
	}
	if item.Status != "pending" || item.Revision != c.ExpectedRevision {
		return s.replayAfterObservationError(ctx, c, ErrConflict)
	}
	confirm := c.Action == "family_suggestion_confirm"
	var documents []weknora.AttachmentDocument
	if confirm {
		if item.EdgeType == "supersedes" && (c.Payload.EffectiveAt == nil || c.Payload.EffectiveAt.UnixMilli() <= 0) {
			return Result{}, ErrInvalid
		}
		for _, pin := range [][2]string{{item.DocumentID, item.SourceVersionID}, {item.TargetDocumentID, item.TargetVersionID}} {
			version, err := s.verify(ctx, c.Scope, pin[0])
			if err != nil {
				return s.replayAfterObservationError(ctx, c, err)
			}
			if version != pin[1] {
				return s.replayAfterObservationError(ctx, c, ErrConflict)
			}
		}
		if item.Kind == "membership" {
			documents, err = s.nativeFamilyPrimaryDocuments(ctx, c.Scope, []string{item.DocumentID})
		} else if item.EdgeType == "supersedes" {
			documents, err = s.familyNativeDocuments(ctx, c.Scope, FamilyReplacement{OldDocumentID: item.TargetDocumentID, NewDocumentID: item.DocumentID})
		}
		if err != nil {
			return s.replayAfterObservationError(ctx, c, err)
		}
	}
	return s.transact(ctx, c, func(tx *sql.Tx) (Result, error) {
		current, err := readSuggestion(ctx, tx, c.Scope, c.ID)
		if err != nil {
			return Result{}, err
		}
		if current.Status != "pending" || current.Revision != c.ExpectedRevision {
			return Result{}, ErrConflict
		}
		status := "ignored"
		var applied any
		if confirm {
			id, err := s.applyFamilySuggestionTx(ctx, tx, c, current, documents)
			if err != nil {
				return Result{}, err
			}
			applied, status = id, "confirmed"
		}
		_, err = tx.ExecContext(ctx, `UPDATE ext_family_suggestions SET status=?,revision=revision+1,applied_id=?
			WHERE tenant_id=? AND knowledge_base_id=? AND suggestion_id=?`, append([]any{status, applied}, args(c.Scope, c.ID)...)...)
		return Result{ID: c.ID, Revision: c.ExpectedRevision + 1, Status: status}, err
	})
}

func (s *Service) applyFamilySuggestionTx(ctx context.Context, tx *sql.Tx, c Command, item FamilySuggestion, documents []weknora.AttachmentDocument) (string, error) {
	p, err := readSuggestionPolicy(ctx, tx, c.Scope)
	if err != nil {
		return "", err
	}
	if item.PolicyRevision != p.Revision || item.Confidence < p.Threshold {
		return "", ErrConflict
	}
	if err := s.suggestionCurrent(ctx, tx, c.Scope, item); err != nil {
		return "", err
	}
	switch {
	case item.Kind == "membership":
		rev, err := suggestionFamilyRevision(ctx, tx, c.Scope, item.BusinessFamilyID)
		if err != nil {
			return "", err
		}
		now := s.now().UTC()
		if err := advanceFamilyRevision(ctx, tx, c.Scope, item.BusinessFamilyID, rev, now); err != nil {
			return "", err
		}
		if err := addFamilyMemberTx(ctx, tx, c.Scope, item.BusinessFamilyID, item.DocumentID, item.SourceVersionID, documents); err != nil {
			return "", err
		}
		return item.BusinessFamilyID, enqueueFamilyProjection(ctx, tx, c, now, []string{item.DocumentID})
	case item.EdgeType == "supersedes":
		rev, err := suggestionFamilyRevision(ctx, tx, c.Scope, item.BusinessFamilyID)
		if err != nil {
			return "", err
		}
		id := newID()
		if id == "" {
			return "", ErrUnavailable
		}
		p := FamilyReplacement{ID: id, BusinessFamilyID: item.BusinessFamilyID, FamilyRevision: rev,
			OldDocumentID: item.TargetDocumentID, OldVersionNodeID: item.TargetVersionID, NewDocumentID: item.DocumentID,
			NewVersionNodeID: item.SourceVersionID, EffectiveAt: *c.Payload.EffectiveAt}
		return id, s.insertFamilyReplacementTx(ctx, tx, c.Scope, p, documents)
	default:
		return s.confirmSuggestedRelationTx(ctx, tx, c.Scope, item)
	}
}

func (s *Service) confirmSuggestedRelationTx(ctx context.Context, tx *sql.Tx, scope provenance.Scope, item FamilySuggestion) (string, error) {
	id := newID()
	if id == "" {
		return "", ErrUnavailable
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO ext_document_relations
		(tenant_id,knowledge_base_id,relation_id,document_id,target_knowledge_base_id,target_document_id,
		 source_version_id,target_version_id,edge_type,status,confidence,revision,edge_id,cross_edge_id)
		SELECT ?,?,?,?,?,?,?,?,?,'active',?,1,NULL,NULL
		WHERE NOT EXISTS(SELECT 1 FROM ext_document_relations WHERE tenant_id=? AND knowledge_base_id=? AND edge_type=?
			AND status='active' AND (source_version_id=? AND target_version_id=? OR edge_type='related_to' AND source_version_id=? AND target_version_id=?))`,
		append(args(scope, id, item.DocumentID, scope.KnowledgeBaseID, item.TargetDocumentID, item.SourceVersionID, item.TargetVersionID, item.EdgeType, item.Confidence),
			args(scope, item.EdgeType, item.SourceVersionID, item.TargetVersionID, item.TargetVersionID, item.SourceVersionID)...)...)
	if err := requireFamilyChange(result, err); err != nil {
		return "", err
	}
	edge := digest([]string{strconvScope(scope), item.SourceVersionID, item.TargetVersionID, item.EdgeType})
	return id, s.activateRelationEdgeTx(ctx, tx, scope, scope, id, item.SourceVersionID, item.TargetVersionID, item.EdgeType, edge)
}
