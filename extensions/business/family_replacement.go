package business

import (
	"context"
	"database/sql"
	"errors"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/dependencies"
	"github.com/ybochuan/LanRAG-Next/extensions/lifecycle"
	"github.com/ybochuan/LanRAG-Next/extensions/lineage"
	"github.com/ybochuan/LanRAG-Next/extensions/provenance"
	"github.com/ybochuan/LanRAG-Next/extensions/store"
	"github.com/ybochuan/LanRAG-Next/extensions/weknora"
)

// FamilyReplacement is an auditable proposal with both observed versions pinned.
// EffectiveAt is the cutover instant: the old document's effective_until and the
// new document's effective_from are derived from it atomically on apply.
type FamilyReplacement struct {
	ID               string     `json:"id"`
	BusinessFamilyID string     `json:"business_family_id"`
	FamilyRevision   int64      `json:"family_revision"`
	OldDocumentID    string     `json:"old_document_id"`
	OldVersionNodeID string     `json:"old_version_node_id"`
	NewDocumentID    string     `json:"new_document_id"`
	NewVersionNodeID string     `json:"new_version_node_id"`
	EffectiveAt      time.Time  `json:"effective_at"`
	Status           string     `json:"status"`
	Revision         int64      `json:"revision"`
	AppliedAt        *time.Time `json:"applied_at,omitempty"`
}

const familyReplacementColumns = `replacement_id,business_family_id,family_revision,old_document_id,old_version_node_id,
	new_document_id,new_version_node_id,effective_at_ms,status,revision,applied_at`

func scanFamilyReplacement(row interface{ Scan(...any) error }) (FamilyReplacement, error) {
	var p FamilyReplacement
	var effectiveMS int64
	var applied sql.NullString
	err := row.Scan(&p.ID, &p.BusinessFamilyID, &p.FamilyRevision, &p.OldDocumentID, &p.OldVersionNodeID,
		&p.NewDocumentID, &p.NewVersionNodeID, &effectiveMS, &p.Status, &p.Revision, &applied)
	if err != nil {
		return p, err
	}
	p.EffectiveAt = time.UnixMilli(effectiveMS).UTC()
	if applied.Valid {
		at, err := time.Parse(time.RFC3339Nano, applied.String)
		if err != nil {
			return p, err
		}
		p.AppliedAt = &at
	}
	return p, nil
}

func readFamilyReplacement(ctx context.Context, tx *sql.Tx, scope provenance.Scope, id string) (FamilyReplacement, error) {
	p, err := scanFamilyReplacement(tx.QueryRowContext(ctx, `SELECT `+familyReplacementColumns+`
		FROM ext_family_replacements WHERE tenant_id=? AND knowledge_base_id=? AND replacement_id=?`, args(scope, id)...))
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrConflict
	}
	return p, err
}

func (s *Service) replaceFamilyMember(ctx context.Context, c Command) (Result, error) {
	if s == nil || s.db == nil {
		return Result{}, ErrUnavailable
	}
	if c.Scope.TenantID == 0 || !validID(c.Scope.KnowledgeBaseID) || !validID(c.ActorID) || !validID(c.OperationID) {
		return Result{}, ErrInvalid
	}
	if r, ok, err := s.replay(ctx, c); ok || err != nil {
		return r, err
	}
	if c.Action == "family_replace_create" {
		return s.createFamilyReplacement(ctx, c)
	}
	if !validID(c.ID) || c.ExpectedRevision < 1 || (c.Action == "family_replace_approve" && !c.Payload.SecondConfirmation) {
		return Result{}, ErrInvalid
	}
	var documents []weknora.AttachmentDocument
	if c.Action == "family_replace_approve" {
		var p FamilyReplacement
		err := s.db.View(ctx, func(tx *sql.Tx) error {
			var err error
			p, err = readFamilyReplacement(ctx, tx, c.Scope, c.ID)
			return err
		})
		if err != nil {
			return Result{}, err
		}
		if p.Status != "pending_review" || p.Revision != c.ExpectedRevision {
			return s.replayAfterObservationError(ctx, c, ErrConflict)
		}
		if !p.EffectiveAt.After(s.now()) {
			return s.replayAfterObservationError(ctx, c, ErrInvalid)
		}
		if err := verifyFamilyReplacement(ctx, c.Scope, p, s.verify); err != nil {
			return s.replayAfterObservationError(ctx, c, err)
		}
		documents, err = s.familyNativeDocuments(ctx, c.Scope, p)
		if err != nil {
			return s.replayAfterObservationError(ctx, c, err)
		}
	}
	return s.transact(ctx, c, func(tx *sql.Tx) (Result, error) {
		p, err := readFamilyReplacement(ctx, tx, c.Scope, c.ID)
		if err != nil {
			return Result{}, err
		}
		if p.Revision != c.ExpectedRevision {
			return Result{}, ErrConflict
		}
		if c.Action == "family_replace_cancel" {
			if p.Status != "pending_review" && p.Status != "scheduled" && p.Status != "needs_review" {
				return Result{}, ErrConflict
			}
			return updateFamilyReplacement(ctx, tx, c.Scope, p, "revoked", nil, nil)
		}
		if p.Status != "pending_review" {
			return Result{}, ErrConflict
		}
		var scheduled bool
		err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM ext_family_replacements
			WHERE tenant_id=? AND knowledge_base_id=? AND business_family_id=? AND status='scheduled')`,
			args(c.Scope, p.BusinessFamilyID)...).Scan(&scheduled)
		if err != nil {
			return Result{}, err
		}
		if scheduled {
			return Result{}, ErrConflict
		}
		if err := s.familyReplacementReady(ctx, tx, c.Scope, p, documents, nil); err != nil {
			return Result{}, err
		}
		return updateFamilyReplacement(ctx, tx, c.Scope, p, "scheduled", nil, nil)
	})
}

func (s *Service) createFamilyReplacement(ctx context.Context, c Command) (Result, error) {
	body := c.Payload
	if c.ID != "" || c.ExpectedRevision != 0 || !validID(body.BusinessFamilyID) || body.FamilyRevision < 1 ||
		!validID(body.DocumentID) || !validID(body.TargetDocumentID) || body.DocumentID == body.TargetDocumentID ||
		body.EffectiveAt == nil || body.EffectiveAt.UnixMilli() <= 0 || !body.EffectiveAt.After(s.now()) {
		return Result{}, ErrInvalid
	}
	p := FamilyReplacement{ID: newID(), BusinessFamilyID: body.BusinessFamilyID, FamilyRevision: body.FamilyRevision,
		OldDocumentID: body.TargetDocumentID, NewDocumentID: body.DocumentID, EffectiveAt: *body.EffectiveAt,
		Status: "pending_review", Revision: 1}
	if p.ID == "" {
		return Result{}, ErrUnavailable
	}
	if err := s.db.View(ctx, func(tx *sql.Tx) error {
		return familyReplacementMembers(ctx, tx, c.Scope, p)
	}); err != nil {
		return s.replayAfterObservationError(ctx, c, err)
	}
	var err error
	p.OldVersionNodeID, err = s.verify(ctx, c.Scope, p.OldDocumentID)
	if err == nil {
		p.NewVersionNodeID, err = s.verify(ctx, c.Scope, p.NewDocumentID)
	}
	if err != nil {
		return s.replayAfterObservationError(ctx, c, err)
	}
	documents, err := s.familyNativeDocuments(ctx, c.Scope, p)
	if err != nil {
		return s.replayAfterObservationError(ctx, c, err)
	}
	return s.transact(ctx, c, func(tx *sql.Tx) (Result, error) {
		err := s.insertFamilyReplacementTx(ctx, tx, c.Scope, p, documents)
		return Result{ID: p.ID, Revision: p.Revision, Status: p.Status}, err
	})
}

func (s *Service) insertFamilyReplacementTx(ctx context.Context, tx *sql.Tx, scope provenance.Scope, p FamilyReplacement, documents []weknora.AttachmentDocument) error {
	if err := s.familyReplacementReady(ctx, tx, scope, p, documents, nil); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO ext_family_replacements
		(tenant_id,knowledge_base_id,replacement_id,business_family_id,family_revision,old_document_id,old_version_node_id,
		 new_document_id,new_version_node_id,effective_at_ms,status,revision)
		VALUES(?,?,?,?,?,?,?,?,?,?,'pending_review',1)`, args(scope, p.ID, p.BusinessFamilyID, p.FamilyRevision,
		p.OldDocumentID, p.OldVersionNodeID, p.NewDocumentID, p.NewVersionNodeID, p.EffectiveAt.UnixMilli())...)
	return err
}

func verifyFamilyReplacement(ctx context.Context, scope provenance.Scope, p FamilyReplacement,
	verify func(context.Context, provenance.Scope, string) (string, error)) error {
	for _, pair := range [][2]string{{p.OldDocumentID, p.OldVersionNodeID}, {p.NewDocumentID, p.NewVersionNodeID}} {
		id, err := verify(ctx, scope, pair[0])
		if errors.Is(err, lineage.ErrSourceMissing) {
			return ErrConflict
		}
		if err != nil {
			return err
		}
		if id != pair[1] {
			return ErrConflict
		}
	}
	return nil
}

func familyReplacementMembers(ctx context.Context, tx *sql.Tx, scope provenance.Scope, p FamilyReplacement) error {
	var revision, members int64
	var document, version string
	err := tx.QueryRowContext(ctx, `SELECT f.revision,COALESCE(h.document_id,''),COALESCE(h.version_node_id,''),
		(SELECT count(*) FROM ext_business_document_family_members m WHERE m.tenant_id=f.tenant_id
		 AND m.knowledge_base_id=f.knowledge_base_id AND m.business_family_id=f.business_family_id AND m.document_id IN (?,?))
		FROM ext_business_document_families f LEFT JOIN ext_business_family_published_heads h USING(tenant_id,knowledge_base_id,business_family_id)
		WHERE f.tenant_id=? AND f.knowledge_base_id=? AND f.business_family_id=?`,
		append([]any{p.OldDocumentID, p.NewDocumentID}, args(scope, p.BusinessFamilyID)...)...).Scan(&revision, &document, &version, &members)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrConflict
	}
	if err != nil {
		return err
	}
	if revision != p.FamilyRevision || members != 2 ||
		(document != "" && (document != p.OldDocumentID || (p.OldVersionNodeID != "" && version != p.OldVersionNodeID))) {
		return ErrConflict
	}
	return nil
}

func (s *Service) familyNativeDocuments(ctx context.Context, scope provenance.Scope, p FamilyReplacement) ([]weknora.AttachmentDocument, error) {
	docs, err := s.nativeFamilyPrimaryDocuments(ctx, scope, []string{p.OldDocumentID, p.NewDocumentID})
	if err != nil {
		return nil, err
	}
	for _, doc := range docs {
		// A change of published head requires explicit enablement. Read-only
		// retrieval still accepts legacy documents with no enablement value.
		if doc.EnableStatus != "enabled" {
			return nil, ErrConflict
		}
	}
	return docs, nil
}

func (s *Service) familyReplacementReady(ctx context.Context, tx *sql.Tx, scope provenance.Scope, p FamilyReplacement,
	documents []weknora.AttachmentDocument, verifiedDependencies map[string]string) error {
	if err := familyReplacementMembers(ctx, tx, scope, p); err != nil {
		return err
	}
	if err := s.current(ctx, tx, scope, p.OldDocumentID, p.OldVersionNodeID); err != nil {
		return err
	}
	newGate := store.PublicationTargetPendingSQL
	if p.Status == "scheduled" {
		// The unique same-family reservation can only be this plan.
		newGate = store.NativePublicationTargetPendingSQL
	}
	if err := s.currentWithPublicationGate(ctx, tx, scope, p.NewDocumentID, p.NewVersionNodeID, newGate); err != nil {
		return err
	}
	for _, doc := range []string{p.OldDocumentID, p.NewDocumentID} {
		if err := requirePrimaryFamilyDocumentTx(ctx, tx, scope, doc); err != nil {
			return err
		}
	}
	for _, doc := range documents {
		version := p.OldVersionNodeID
		if doc.ID == p.NewDocumentID {
			version = p.NewVersionNodeID
		}
		if err := requireFamilyHeaderTx(ctx, tx, scope, doc, version); err != nil {
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
	for _, pair := range [][2]string{{p.OldDocumentID, p.NewDocumentID}, {p.NewDocumentID, p.OldDocumentID}} {
		assessment, err := dependencies.LoadTx(ctx, tx, dependencies.Scope{
			TenantID: scope.TenantID, KnowledgeBaseID: scope.KnowledgeBaseID, DocumentID: pair[0],
		})
		if err != nil {
			return err
		}
		for _, ref := range assessment.Sources {
			if ref.DocumentID == pair[1] || (verifiedDependencies != nil && verifiedDependencies[ref.DocumentID] != ref.VersionID) {
				return ErrConflict
			}
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

func updateFamilyReplacement(ctx context.Context, tx *sql.Tx, scope provenance.Scope, p FamilyReplacement, status string, event, at any) (Result, error) {
	_, err := tx.ExecContext(ctx, `UPDATE ext_family_replacements SET status=?,revision=revision+1,applied_event_id=?,applied_at=?
		WHERE tenant_id=? AND knowledge_base_id=? AND replacement_id=?`, append([]any{status, event, at}, args(scope, p.ID)...)...)
	return Result{ID: p.ID, Revision: p.Revision + 1, Status: status}, err
}

func (s *Service) applyFamilyReplacement(ctx context.Context, tx *sql.Tx, c Command, p FamilyReplacement) (Result, error) {
	now := s.now().UTC()
	out, err := s.applyFamilyReplacementFacts(ctx, tx, c.Scope, p, now)
	if err != nil {
		return Result{}, err
	}
	if err := enqueueFamilyProjectionIfEnabled(ctx, tx, c, now, []string{p.OldDocumentID, p.NewDocumentID}); err != nil {
		return Result{}, err
	}
	return out, s.current(ctx, tx, c.Scope, p.NewDocumentID, p.NewVersionNodeID)
}

// applyFamilyReplacementAnnotations makes the validity boundaries agree with
// the cutover instant. EffectiveAt is inclusive for effective_until in the
// shared EffectiveAt helper, so the old member ends one nanosecond earlier.
func applyFamilyReplacementAnnotations(ctx context.Context, tx *sql.Tx, scope provenance.Scope, p FamilyReplacement) error {
	if p.EffectiveAt.IsZero() {
		return ErrInvalid
	}
	effective := p.EffectiveAt.UTC()
	oldUntil := effective.Add(-time.Nanosecond)
	old, err := lifecycle.ReadAnnotationsTx(ctx, tx, lifecycle.Scope{
		TenantID: scope.TenantID, KnowledgeBaseID: scope.KnowledgeBaseID, DocumentID: p.OldDocumentID,
	})
	if err != nil {
		return err
	}
	if old.EffectiveFrom != nil && !oldUntil.After(*old.EffectiveFrom) {
		return ErrConflict
	}
	old.EffectiveUntil = &oldUntil
	if err := lifecycle.SaveAnnotationsTx(ctx, tx, lifecycle.Scope{
		TenantID: scope.TenantID, KnowledgeBaseID: scope.KnowledgeBaseID, DocumentID: p.OldDocumentID,
	}, old, old.Revision); err != nil {
		return err
	}
	next, err := lifecycle.ReadAnnotationsTx(ctx, tx, lifecycle.Scope{
		TenantID: scope.TenantID, KnowledgeBaseID: scope.KnowledgeBaseID, DocumentID: p.NewDocumentID,
	})
	if err != nil {
		return err
	}
	if next.EffectiveUntil != nil && !next.EffectiveUntil.After(effective) {
		return ErrConflict
	}
	next.EffectiveFrom = &effective
	if err := lifecycle.SaveAnnotationsTx(ctx, tx, lifecycle.Scope{
		TenantID: scope.TenantID, KnowledgeBaseID: scope.KnowledgeBaseID, DocumentID: p.NewDocumentID,
	}, next, next.Revision); err != nil {
		return err
	}
	return nil
}

func (s *Service) applyFamilyReplacementFacts(ctx context.Context, tx *sql.Tx, scope provenance.Scope, p FamilyReplacement, now time.Time) (Result, error) {
	stamp := now.Format(time.RFC3339Nano)
	effective := p.EffectiveAt.UTC()
	effectiveStamp := effective.Format(time.RFC3339Nano)
	event := "family-replace-" + p.ID
	if err := applyFamilyReplacementAnnotations(ctx, tx, scope, p); err != nil {
		return Result{}, err
	}
	if err := s.retireRevisionSource(ctx, tx, Publication{Scope: scope, SourceDocumentID: p.OldDocumentID}, event, "family_replacement_effective", effective); err != nil {
		return Result{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO ext_lineage_edge VALUES(?,?,?,?,?,'supersedes','governance_critical',?)`,
		append([]any{event}, args(scope, p.NewVersionNodeID, p.OldVersionNodeID, effectiveStamp)...)...); err != nil {
		return Result{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO ext_business_document_family_heads
		(tenant_id,knowledge_base_id,business_family_id,document_id,version_node_id,replacement_id) VALUES(?,?,?,?,?,?)
		ON CONFLICT(tenant_id,knowledge_base_id,business_family_id) DO UPDATE SET
		document_id=excluded.document_id,version_node_id=excluded.version_node_id,replacement_id=excluded.replacement_id`,
		args(scope, p.BusinessFamilyID, p.NewDocumentID, p.NewVersionNodeID, p.ID)...); err != nil {
		return Result{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE ext_business_document_families SET revision=revision+1,updated_at=?
		WHERE tenant_id=? AND knowledge_base_id=? AND business_family_id=?`, append([]any{stamp}, args(scope, p.BusinessFamilyID)...)...); err != nil {
		return Result{}, err
	}
	return updateFamilyReplacement(ctx, tx, scope, p, "applied", event, stamp)
}

// SweepFamilyReplacements executes due cutovers in the configured Host scopes.
func (s *Service) SweepFamilyReplacements(ctx context.Context, scopes []provenance.Scope) error {
	if s == nil || s.db == nil {
		return ErrUnavailable
	}
	cutoff := s.now().UnixMilli()
	var failures []error
	for _, scope := range scopes {
		var cursorMS int64
		var cursorID string
		for {
			page, err := s.familyReplacementPage(ctx, `WHERE tenant_id=? AND knowledge_base_id=? AND status='scheduled'
				AND effective_at_ms<=? AND (effective_at_ms,replacement_id)>(?,?) ORDER BY effective_at_ms,replacement_id LIMIT 64`,
				args(scope, cutoff, cursorMS, cursorID)...)
			if err != nil {
				return errors.Join(append(failures, err)...)
			}
			for _, p := range page {
				cursorMS, cursorID = p.EffectiveAt.UnixMilli(), p.ID
				if err := s.sweepFamilyReplacement(ctx, scope, p); err != nil {
					failures = append(failures, err)
				}
				if ctx.Err() != nil {
					return ctx.Err()
				}
			}
			if len(page) < 64 {
				break
			}
		}
	}
	return errors.Join(failures...)
}

func (s *Service) sweepFamilyReplacement(ctx context.Context, scope provenance.Scope, p FamilyReplacement) error {
	observed := verifyFamilyReplacement(ctx, scope, p, s.observeRepealVersion)
	var documents []weknora.AttachmentDocument
	var verified map[string]string
	if observed == nil {
		documents, observed = s.familyNativeDocuments(ctx, scope, p)
	}
	if observed == nil {
		verified, observed = s.observeFamilyDependencies(ctx, scope, p)
	}
	if observed != nil && !errors.Is(observed, ErrConflict) {
		return observed
	}
	c := Command{Scope: scope, ID: p.ID, ExpectedRevision: p.Revision, ActorID: "system:family",
		OperationID: "family-apply-" + newID(), Action: "family_replace_apply"}
	advanced := false
	_, err := s.transact(ctx, c, func(tx *sql.Tx) (Result, error) {
		current, err := readFamilyReplacement(ctx, tx, scope, p.ID)
		if err != nil {
			return Result{}, err
		}
		if current.Status != "scheduled" || current.Revision != p.Revision || current.EffectiveAt.After(s.now()) {
			advanced = true
			return Result{}, ErrConflict
		}
		if observed == nil {
			observed = s.familyReplacementReady(ctx, tx, scope, current, documents, verified)
		}
		if errors.Is(observed, ErrConflict) {
			return updateFamilyReplacement(ctx, tx, scope, current, "needs_review", nil, nil)
		}
		if observed != nil {
			return Result{}, observed
		}
		return s.applyFamilyReplacement(ctx, tx, c, current)
	})
	if advanced && errors.Is(err, ErrConflict) {
		return nil
	}
	return err
}

func (s *Service) observeFamilyDependencies(ctx context.Context, scope provenance.Scope, p FamilyReplacement) (map[string]string, error) {
	pins := map[string]string{p.OldDocumentID: p.OldVersionNodeID, p.NewDocumentID: p.NewVersionNodeID}
	err := s.db.View(ctx, func(tx *sql.Tx) error {
		for _, doc := range []string{p.OldDocumentID, p.NewDocumentID} {
			a, err := dependencies.LoadTx(ctx, tx, dependencies.Scope{
				TenantID: scope.TenantID, KnowledgeBaseID: scope.KnowledgeBaseID, DocumentID: doc,
			})
			if err != nil {
				return err
			}
			for _, ref := range a.Sources {
				if pin := pins[ref.DocumentID]; pin != "" && pin != ref.VersionID {
					return ErrConflict
				}
				pins[ref.DocumentID] = ref.VersionID
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	for _, doc := range slices.Sorted(maps.Keys(pins)) {
		if doc == p.OldDocumentID || doc == p.NewDocumentID {
			continue // Both endpoints were freshly observed before reading dependencies.
		}
		version, err := s.observeRepealVersion(ctx, scope, doc)
		if errors.Is(err, lineage.ErrSourceMissing) {
			return nil, ErrConflict
		}
		if err != nil {
			return nil, err
		}
		if version != pins[doc] {
			return nil, ErrConflict
		}
	}
	return pins, nil
}

func (s *Service) familyReplacementPage(ctx context.Context, tail string, values ...any) ([]FamilyReplacement, error) {
	items := []FamilyReplacement{}
	err := s.db.View(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, "SELECT "+familyReplacementColumns+" FROM ext_family_replacements "+tail, values...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			p, err := scanFamilyReplacement(rows)
			if err != nil {
				return err
			}
			items = append(items, p)
		}
		return rows.Err()
	})
	return items, err
}

// FamilyReplacements lists a scoped history, optionally restricted to one family.
func (s *Service) FamilyReplacements(ctx context.Context, c Command) ([]FamilyReplacement, error) {
	if s == nil || s.db == nil {
		return nil, ErrUnavailable
	}
	limit, offset, err := familyPage(c)
	if err != nil {
		return nil, err
	}
	order := "effective_at_ms DESC,replacement_id"
	if c.ListStatus == "timeline" {
		order = "effective_at_ms,replacement_id"
	}
	return s.familyReplacementPage(ctx, `WHERE tenant_id=? AND knowledge_base_id=? AND (?='' OR business_family_id=?)
		ORDER BY `+order+` LIMIT ? OFFSET ?`, args(c.Scope, c.ID, c.ID, limit, offset)...)
}
