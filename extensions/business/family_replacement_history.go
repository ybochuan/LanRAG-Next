package business

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"

	"github.com/ybochuan/LanRAG-Next/extensions/provenance"
	"github.com/ybochuan/LanRAG-Next/extensions/weknora"
)

const maxHistoricalReplacements = 16

// FamilyReplacementHistory describes a past-due suffix of one family's timeline.
// Fingerprint is present only when the saved drafts form a confirmable chain.
type FamilyReplacementHistory struct {
	FamilyID            string              `json:"business_family_id"`
	FamilyRevision      int64               `json:"family_revision"`
	PublishedDocumentID string              `json:"published_document_id,omitempty"`
	FinalDocumentID     string              `json:"final_document_id,omitempty"`
	Steps               []FamilyReplacement `json:"steps"`
	Fingerprint         string              `json:"fingerprint,omitempty"`
	BlockedReason       string              `json:"blocked_reason,omitempty"`
}

// FamilyReplacementHistory reads a preview without refreshing or changing facts.
func (s *Service) FamilyReplacementHistory(ctx context.Context, c Command) (FamilyReplacementHistory, error) {
	if s == nil || s.db == nil {
		return FamilyReplacementHistory{}, ErrUnavailable
	}
	if c.Scope.TenantID == 0 || !validID(c.Scope.KnowledgeBaseID) || !validID(c.ID) {
		return FamilyReplacementHistory{}, ErrInvalid
	}
	var preview FamilyReplacementHistory
	err := s.db.View(ctx, func(tx *sql.Tx) error {
		var err error
		preview, err = s.familyReplacementHistoryTx(ctx, tx, c.Scope, c.ID)
		return err
	})
	return preview, err
}

func (s *Service) familyReplacementHistoryTx(ctx context.Context, tx *sql.Tx, scope provenance.Scope, familyID string) (FamilyReplacementHistory, error) {
	preview := FamilyReplacementHistory{FamilyID: familyID, Steps: []FamilyReplacement{}}
	var headVersion string
	var headDate int64
	var reserved bool
	err := tx.QueryRowContext(ctx, `SELECT f.revision,COALESCE(h.document_id,''),COALESCE(h.version_node_id,''),
		COALESCE(r.effective_at_ms,CAST(strftime('%s',lm.applied_at) AS INTEGER)*1000,0),
		EXISTS(SELECT 1 FROM ext_family_replacements p WHERE p.tenant_id=f.tenant_id
		AND p.knowledge_base_id=f.knowledge_base_id AND p.business_family_id=f.business_family_id AND p.status='scheduled')
		FROM ext_business_document_families f
		LEFT JOIN ext_business_family_published_heads h USING(tenant_id,knowledge_base_id,business_family_id)
		LEFT JOIN ext_business_document_family_heads hh ON hh.tenant_id=f.tenant_id
		AND hh.knowledge_base_id=f.knowledge_base_id AND hh.business_family_id=f.business_family_id
		LEFT JOIN ext_family_replacements r ON r.tenant_id=hh.tenant_id AND r.knowledge_base_id=hh.knowledge_base_id
		AND r.business_family_id=hh.business_family_id AND r.replacement_id=hh.replacement_id AND r.status='applied'
		LEFT JOIN ext_business_family_legacy_mappings lm ON lm.tenant_id=f.tenant_id
		AND lm.knowledge_base_id=f.knowledge_base_id AND lm.business_family_id=f.business_family_id
		WHERE f.tenant_id=? AND f.knowledge_base_id=? AND f.business_family_id=?`, args(scope, familyID)...).Scan(
		&preview.FamilyRevision, &preview.PublishedDocumentID, &headVersion, &headDate, &reserved)
	if errors.Is(err, sql.ErrNoRows) {
		return preview, ErrNotFound
	}
	if err != nil {
		return preview, err
	}
	if headDate == 0 {
		preview.BlockedReason = "missing_applied_head"
		return preview, nil
	}
	if reserved {
		preview.BlockedReason = "scheduled_replacement"
		return preview, nil
	}
	rows, err := tx.QueryContext(ctx, `SELECT `+familyReplacementColumns+` FROM ext_family_replacements
		WHERE tenant_id=? AND knowledge_base_id=? AND business_family_id=? AND status IN ('pending_review','needs_review')
		AND effective_at_ms<=? ORDER BY effective_at_ms,replacement_id LIMIT ?`,
		args(scope, familyID, s.now().UnixMilli(), maxHistoricalReplacements+1)...)
	if err != nil {
		return preview, err
	}
	defer rows.Close()
	for rows.Next() {
		p, err := scanFamilyReplacement(rows)
		if err != nil {
			return preview, err
		}
		preview.Steps = append(preview.Steps, p)
	}
	if err := rows.Err(); err != nil {
		return preview, err
	}
	if err := rows.Close(); err != nil {
		return preview, err
	}
	switch {
	case len(preview.Steps) == 0:
		preview.BlockedReason = "no_past_drafts"
		return preview, nil
	case len(preview.Steps) > maxHistoricalReplacements:
		preview.BlockedReason = "history_limit"
		return preview, nil
	}
	previous, version, date := preview.PublishedDocumentID, headVersion, headDate
	seen := map[string]bool{previous: true}
	for _, p := range preview.Steps {
		if p.Status != "pending_review" || p.OldDocumentID != previous || p.OldVersionNodeID != version ||
			p.EffectiveAt.UnixMilli() <= date || seen[p.NewDocumentID] {
			preview.BlockedReason = "disconnected_history"
			return preview, nil
		}
		seen[p.NewDocumentID] = true
		previous, version, date = p.NewDocumentID, p.NewVersionNodeID, p.EffectiveAt.UnixMilli()
	}
	for _, p := range preview.Steps {
		if err := s.current(ctx, tx, scope, p.OldDocumentID, p.OldVersionNodeID); err != nil {
			if !errors.Is(err, ErrConflict) && !errors.Is(err, ErrUnavailable) {
				return preview, err
			}
			preview.BlockedReason = "document_needs_review"
			return preview, nil
		}
	}
	if err := s.current(ctx, tx, scope, previous, version); err != nil {
		if !errors.Is(err, ErrConflict) && !errors.Is(err, ErrUnavailable) {
			return preview, err
		}
		preview.BlockedReason = "document_needs_review"
		return preview, nil
	}
	preview.FinalDocumentID = previous
	preview.Fingerprint = digest(preview)
	return preview, nil
}

func (p FamilyReplacementHistory) matches(c Command) bool {
	if p.Fingerprint == "" || p.Fingerprint != c.Payload.HistoryFingerprint ||
		p.FamilyRevision != c.ExpectedRevision || len(p.Steps) != len(c.Payload.ReplacementIDs) {
		return false
	}
	for i, step := range p.Steps {
		if step.ID != c.Payload.ReplacementIDs[i] {
			return false
		}
	}
	return true
}

func (s *Service) applyFamilyReplacementHistory(ctx context.Context, c Command) (Result, error) {
	if s == nil || s.db == nil {
		return Result{}, ErrUnavailable
	}
	fingerprint, err := hex.DecodeString(c.Payload.HistoryFingerprint)
	if !validID(c.ID) || !validID(c.OperationID) || !validID(c.ActorID) || c.ExpectedRevision < 1 ||
		!c.Payload.SecondConfirmation || err != nil || len(fingerprint) != 32 {
		return Result{}, ErrInvalid
	}
	if r, ok, err := s.replay(ctx, c); ok || err != nil {
		return r, err
	}
	preview, err := s.FamilyReplacementHistory(ctx, c)
	if err != nil {
		return s.replayAfterObservationError(ctx, c, err)
	}
	if !preview.matches(c) {
		return s.replayAfterObservationError(ctx, c, ErrConflict)
	}
	docs := []string{preview.PublishedDocumentID}
	evidence := make([][]weknora.AttachmentDocument, len(preview.Steps))
	dependencies := make([]map[string]string, len(preview.Steps))
	for i, p := range preview.Steps {
		docs = append(docs, p.NewDocumentID)
		if err := verifyFamilyReplacement(ctx, c.Scope, p, s.verify); err != nil {
			return s.replayAfterObservationError(ctx, c, err)
		}
		evidence[i], err = s.familyNativeDocuments(ctx, c.Scope, p)
		if err != nil {
			return s.replayAfterObservationError(ctx, c, err)
		}
		dependencies[i], err = s.observeFamilyDependencies(ctx, c.Scope, p)
		if err != nil {
			return s.replayAfterObservationError(ctx, c, err)
		}
	}
	return s.transact(ctx, c, func(tx *sql.Tx) (Result, error) {
		current, err := s.familyReplacementHistoryTx(ctx, tx, c.Scope, c.ID)
		if err != nil {
			return Result{}, err
		}
		if !current.matches(c) {
			return Result{}, ErrConflict
		}
		now := s.now().UTC()
		for i, p := range current.Steps {
			// The approved chain advances the revision and head inside this transaction.
			p.FamilyRevision = current.FamilyRevision + int64(i)
			if err := s.familyReplacementReady(ctx, tx, c.Scope, p, evidence[i], dependencies[i]); err != nil {
				return Result{}, err
			}
			if _, err := s.applyFamilyReplacementFacts(ctx, tx, c.Scope, p, now); err != nil {
				return Result{}, err
			}
		}
		last := current.Steps[len(current.Steps)-1]
		if err := s.current(ctx, tx, c.Scope, last.NewDocumentID, last.NewVersionNodeID); err != nil {
			return Result{}, err
		}
		if err := enqueueFamilyProjectionIfEnabled(ctx, tx, c, now, docs); err != nil {
			return Result{}, err
		}
		return Result{ID: c.ID, Revision: current.FamilyRevision + int64(len(current.Steps)),
			Status: "applied", TargetDocumentID: last.NewDocumentID}, nil
	})
}
