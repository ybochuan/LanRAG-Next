package business

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/dependencies"
	"github.com/ybochuan/LanRAG-Next/extensions/lineage"
	"github.com/ybochuan/LanRAG-Next/extensions/provenance"
	"github.com/ybochuan/LanRAG-Next/extensions/store"
	"github.com/ybochuan/LanRAG-Next/extensions/weknora"
)

// recoverFamilyHead repairs a family whose recorded published head is absent
// from the native knowledge base. It never infers a successor: the caller must
// name an existing member, confirm explicitly, and advance the family CAS.
func (s *Service) recoverFamilyHead(ctx context.Context, c Command) (Result, error) {
	if s == nil || s.db == nil {
		return Result{}, ErrUnavailable
	}
	p := c.Payload
	if c.Scope.TenantID == 0 || !validID(c.Scope.KnowledgeBaseID) || !validID(c.ActorID) ||
		!validID(c.OperationID) || !validID(c.ID) || c.ExpectedRevision < 1 ||
		!validID(p.DocumentID) || p.Reason == "" || len(p.Reason) > 512 || !p.SecondConfirmation ||
		p.FamilyLabel != "" || p.BusinessFamilyID != "" || p.TargetDocumentID != "" ||
		strings.IndexFunc(p.Reason, func(r rune) bool { return r < 0x20 || r == 0x7f }) >= 0 {
		return Result{}, ErrInvalid
	}
	if r, ok, err := s.replay(ctx, c); ok || err != nil {
		return r, err
	}
	var familyRevision int64
	var oldDocument, oldVersion string
	err := s.db.View(ctx, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `SELECT f.revision,COALESCE(h.document_id,''),COALESCE(h.version_node_id,'')
			FROM ext_business_document_families f
			LEFT JOIN ext_business_family_published_heads h USING(tenant_id,knowledge_base_id,business_family_id)
			WHERE f.tenant_id=? AND f.knowledge_base_id=? AND f.business_family_id=?`,
			args(c.Scope, c.ID)...).Scan(&familyRevision, &oldDocument, &oldVersion)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return Result{}, ErrNotFound
	}
	if err != nil {
		return Result{}, err
	}
	if oldDocument == "" || oldVersion == "" || oldDocument == p.DocumentID {
		return Result{}, ErrConflict
	}
	present, err := s.nativeFamilyDocumentPresent(ctx, c.Scope, oldDocument)
	if err != nil {
		return s.replayAfterObservationError(ctx, c, err)
	}
	if present {
		return Result{}, ErrConflict
	}
	docs, err := s.nativeFamilyPrimaryDocuments(ctx, c.Scope, []string{p.DocumentID})
	if err != nil {
		return s.replayAfterObservationError(ctx, c, err)
	}
	if len(docs) == 0 && s.registry != nil {
		return Result{}, ErrUnavailable
	}
	for _, doc := range docs {
		if doc.EnableStatus != "enabled" {
			return Result{}, ErrConflict
		}
	}
	newVersion, err := s.verify(ctx, c.Scope, p.DocumentID)
	if err != nil {
		return s.replayAfterObservationError(ctx, c, err)
	}
	now := s.now().UTC()
	replacement := FamilyReplacement{
		ID:               c.OperationID,
		BusinessFamilyID: c.ID,
		FamilyRevision:   familyRevision,
		OldDocumentID:    oldDocument,
		OldVersionNodeID: oldVersion,
		NewDocumentID:    p.DocumentID,
		NewVersionNodeID: newVersion,
		EffectiveAt:      now,
		Status:           "pending_review",
		Revision:         1,
	}
	return s.transact(ctx, c, func(tx *sql.Tx) (Result, error) {
		if err := s.familyRecoveryReady(ctx, tx, c.Scope, replacement, docs); err != nil {
			return Result{}, err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO ext_family_replacements
			(tenant_id,knowledge_base_id,replacement_id,business_family_id,family_revision,old_document_id,old_version_node_id,
			 new_document_id,new_version_node_id,effective_at_ms,status,revision)
			VALUES(?,?,?,?,?,?,?,?,?,?,'pending_review',1)`,
			args(c.Scope, replacement.ID, replacement.BusinessFamilyID, replacement.FamilyRevision,
				replacement.OldDocumentID, replacement.OldVersionNodeID, replacement.NewDocumentID,
				replacement.NewVersionNodeID, replacement.EffectiveAt.UnixMilli())...); err != nil {
			return Result{}, err
		}
		out, err := s.applyFamilyReplacementFacts(ctx, tx, c.Scope, replacement, now)
		if err != nil {
			return Result{}, err
		}
		if err := recordDocumentAvailabilityTx(ctx, tx, c.Scope, oldDocument, documentAvailabilityMissing, "family_head_recovery_source_missing", now.Format(time.RFC3339Nano)); err != nil {
			return Result{}, err
		}
		if err := recordDocumentAvailabilityTx(ctx, tx, c.Scope, p.DocumentID, documentAvailabilityActive, "", now.Format(time.RFC3339Nano)); err != nil {
			return Result{}, err
		}
		if err := enqueueFamilyProjectionIfEnabled(ctx, tx, c, now, []string{oldDocument, p.DocumentID}); err != nil {
			return Result{}, err
		}
		return out, nil
	})
}

func (s *Service) familyRecoveryReady(ctx context.Context, tx *sql.Tx, scope provenance.Scope,
	p FamilyReplacement, documents []weknora.AttachmentDocument) error {
	var revision int64
	var oldDocument, oldVersion string
	err := tx.QueryRowContext(ctx, `SELECT f.revision,COALESCE(h.document_id,''),COALESCE(h.version_node_id,'')
		FROM ext_business_document_families f
		LEFT JOIN ext_business_family_published_heads h USING(tenant_id,knowledge_base_id,business_family_id)
		WHERE f.tenant_id=? AND f.knowledge_base_id=? AND f.business_family_id=?`,
		args(scope, p.BusinessFamilyID)...).Scan(&revision, &oldDocument, &oldVersion)
	if errors.Is(err, sql.ErrNoRows) || err == nil && (revision != p.FamilyRevision || oldDocument != p.OldDocumentID || oldVersion != p.OldVersionNodeID) {
		return ErrConflict
	}
	if err != nil {
		return err
	}
	var member bool
	err = tx.QueryRowContext(ctx, `SELECT EXISTS(
		SELECT 1 FROM ext_business_document_family_members m
		JOIN ext_business_family_member_history h
		  ON h.tenant_id=m.tenant_id AND h.knowledge_base_id=m.knowledge_base_id
		 AND h.business_family_id=m.business_family_id AND h.document_id=m.document_id
		WHERE m.tenant_id=? AND m.knowledge_base_id=? AND m.business_family_id=?
		  AND m.document_id=? AND h.status='active')`, args(scope, p.BusinessFamilyID, p.NewDocumentID)...).Scan(&member)
	if err != nil {
		return err
	}
	if !member {
		return ErrConflict
	}
	if err := s.currentWithPublicationGate(ctx, tx, scope, p.NewDocumentID, p.NewVersionNodeID, store.PublicationTargetPendingSQL); err != nil {
		return err
	}
	if err := requirePrimaryFamilyDocumentTx(ctx, tx, scope, p.NewDocumentID); err != nil {
		return err
	}
	for _, doc := range documents {
		if doc.ID != p.NewDocumentID {
			continue
		}
		if err := requireFamilyHeaderTx(ctx, tx, scope, doc, p.NewVersionNodeID); err != nil {
			return err
		}
		held, err := store.LegacyPublicationHeldTx(ctx, tx, doc)
		if err != nil {
			return err
		}
		if held {
			return ErrConflict
		}
		if strings.HasPrefix(doc.Channel, weknora.RevisionPublicationChannelPrefix) || strings.HasPrefix(doc.Channel, weknora.SplitPublicationChannelPrefix) {
			var channel string
			if err := tx.QueryRowContext(ctx, `SELECT COALESCE(`+store.PublishedPublicationChannelSQL+`,'')
				FROM ext_lifecycle_current c WHERE c.tenant_id=? AND c.knowledge_base_id=? AND c.document_id=?`,
				args(scope, doc.ID)...).Scan(&channel); err != nil {
				return err
			}
			if doc.Channel != channel {
				return ErrConflict
			}
		}
	}
	assessment, err := dependencies.LoadTx(ctx, tx, dependencies.Scope{
		TenantID: scope.TenantID, KnowledgeBaseID: scope.KnowledgeBaseID, DocumentID: p.NewDocumentID,
	})
	if err != nil || assessment.ReadBlocked(s.now()) {
		return ErrConflict
	}
	for _, ref := range assessment.Sources {
		if ref.DocumentID == p.OldDocumentID {
			return ErrConflict
		}
	}
	if err := lineage.CheckVersionLinkTx(ctx, tx, scope, p.NewVersionNodeID, p.OldVersionNodeID); err != nil {
		if errors.Is(err, lineage.ErrConflict) || errors.Is(err, lineage.ErrTraversalLimit) {
			return ErrConflict
		}
		return err
	}
	return nil
}

func (s *Service) nativeFamilyDocumentPresent(ctx context.Context, scope provenance.Scope, id string) (bool, error) {
	native, ok := s.remote.(*NativePublisher)
	if !ok || native == nil || native.client == nil {
		if s.registry == nil {
			return false, nil
		}
		return false, ErrUnavailable
	}
	docs, err := native.client.AttachmentDocuments(ctx, []string{scope.KnowledgeBaseID}, []string{id})
	if err != nil {
		return false, ErrUnavailable
	}
	return len(docs) != 0, nil
}
