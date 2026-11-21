package business

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/corrections"
	"github.com/ybochuan/LanRAG-Next/extensions/provenance"
)

func correctionScope(scope provenance.Scope) corrections.Scope {
	return corrections.Scope{TenantID: scope.TenantID, KnowledgeBaseID: scope.KnowledgeBaseID}
}

func (s *Service) verifyReviewedCorrectionPeers(ctx context.Context, tx *sql.Tx, c Command, pin *FeedbackCitation) error {
	peers := c.Payload.ReviewedCorrectionPeers
	if len(peers) > maxFeedbackCitations {
		return ErrInvalid
	}
	seen := make(map[string]bool, len(peers))
	for _, peer := range peers {
		if !validID(peer.ID) || peer.Revision <= 0 || peer.ID == c.Payload.SupersedesCorrectionID || seen[peer.ID] {
			return ErrInvalid
		}
		seen[peer.ID] = true
		active, err := corrections.ActiveTx(ctx, tx, correctionScope(c.Scope), s.now(), pin.DocumentID, pin.VersionID, peer.ID)
		if errors.Is(err, corrections.ErrNotFound) {
			return ErrConflict
		}
		if err != nil {
			return err
		}
		if active.Revision != peer.Revision || active.ChunkID != pin.ChunkID {
			return ErrConflict
		}
	}
	return nil
}

func (s *Service) stageCorrectionReplacement(ctx context.Context, tx *sql.Tx, c Command, pin *FeedbackCitation) error {
	previous, revision := c.Payload.SupersedesCorrectionID, c.Payload.SupersedesCorrectionRevision
	if previous == "" && revision == 0 {
		return nil
	}
	if !validID(previous) || revision <= 0 || previous == c.ID {
		return ErrInvalid
	}
	entry, err := corrections.ActiveTx(ctx, tx, correctionScope(c.Scope), s.now(), pin.DocumentID, pin.VersionID, previous)
	if errors.Is(err, corrections.ErrNotFound) {
		return ErrConflict
	}
	if err != nil {
		return err
	}
	if entry.Revision != revision || entry.ChunkID != pin.ChunkID {
		return ErrConflict
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO ext_feedback_correction_replacements(tenant_id,knowledge_base_id,feedback_id,previous_id,previous_revision) VALUES(?,?,?,?,?)`, args(c.Scope, c.ID, previous, revision)...)
	return err
}

// publishCorrection commits the activation and any explicit replacement together.
func (s *Service) publishCorrection(ctx context.Context, tx *sql.Tx, c Command, pin *FeedbackCitation, now time.Time) error {
	key := args(c.Scope, c.ID)
	inserted, err := tx.ExecContext(ctx, `INSERT INTO ext_feedback_corrections
 (tenant_id,knowledge_base_id,feedback_id,document_id,version_id,chunk_id,chunk_index,content_hash,correction,applies_when,approved_at,effective_at)
 SELECT q.tenant_id,q.knowledge_base_id,q.feedback_id,?,?,?,?,?,q.reviewed_guidance,q.applies_when,e.occurred_at,?
 FROM ext_qa_feedback q JOIN ext_feedback_events e ON (e.tenant_id,e.knowledge_base_id,e.event_id)=(q.tenant_id,q.knowledge_base_id,q.feedback_id)
 WHERE q.tenant_id=? AND q.knowledge_base_id=? AND q.feedback_id=? AND q.status='approved' AND q.rating IN ('dislike','supplement','correction')`,
		append([]any{pin.DocumentID, pin.VersionID, pin.ChunkID, pin.ChunkIndex, pin.ContentHash, now.UTC().Format(time.RFC3339Nano)}, key...)...)
	if err != nil {
		return err
	}
	n, err := inserted.RowsAffected()
	if err != nil {
		return err
	}
	var previous string
	var revision int64
	replacementErr := tx.QueryRowContext(ctx, `SELECT previous_id,previous_revision FROM ext_feedback_correction_replacements WHERE tenant_id=? AND knowledge_base_id=? AND feedback_id=?`, key...).Scan(&previous, &revision)
	if replacementErr != nil && !errors.Is(replacementErr, sql.ErrNoRows) {
		return replacementErr
	}
	if n == 0 {
		if replacementErr == nil {
			return ErrConflict
		}
		return nil // Non-fact QA feedback stays on the legacy path.
	}
	entry, err := corrections.ActiveTx(ctx, tx, correctionScope(c.Scope), now, pin.DocumentID, pin.VersionID, c.ID)
	if err != nil {
		if errors.Is(err, corrections.ErrNotFound) {
			return ErrConflict
		}
		return err
	}
	conflict, err := corrections.ConflictsTx(ctx, tx, correctionScope(c.Scope), now, entry, previous)
	if err != nil {
		return err
	}
	if conflict {
		return ErrConflict
	}
	if errors.Is(replacementErr, sql.ErrNoRows) {
		return nil
	}
	old, err := corrections.ActiveTx(ctx, tx, correctionScope(c.Scope), now, pin.DocumentID, pin.VersionID, previous)
	if errors.Is(err, corrections.ErrNotFound) {
		return ErrConflict
	}
	if err != nil {
		return err
	}
	if old.Revision != revision || old.ChunkID != pin.ChunkID {
		return ErrConflict
	}
	previousKey := args(c.Scope, previous)
	if _, err = tx.ExecContext(ctx, `UPDATE ext_feedback_corrections SET superseded_by=? WHERE tenant_id=? AND knowledge_base_id=? AND feedback_id=?`, append([]any{c.ID}, previousKey...)...); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE ext_feedback_staging SET status='withdrawn',ready_at=NULL,revision=revision+1 WHERE tenant_id=? AND knowledge_base_id=? AND feedback_id=?`, previousKey...); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE ext_feedback_guidances SET status='withdrawn',revision=revision+1 WHERE tenant_id=? AND knowledge_base_id=? AND guidance_id=?`, previousKey...)
	return err
}
