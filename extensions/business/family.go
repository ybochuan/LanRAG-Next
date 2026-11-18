package business

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/ybochuan/LanRAG-Next/extensions/provenance"
	"github.com/ybochuan/LanRAG-Next/extensions/store"
	"github.com/ybochuan/LanRAG-Next/extensions/weknora"
)

// BusinessFamily is an explicitly registered family within one knowledge base.
type BusinessFamily struct {
	ID                        string `json:"business_family_id"`
	Label                     string `json:"label"`
	Revision                  int64  `json:"revision"`
	PublishedDocumentID       string `json:"published_document_id,omitempty"`
	PublishedVersionNodeID    string `json:"published_version_node_id,omitempty"`
	PublishedHeadAvailability string `json:"published_head_availability,omitempty"`
}

// FamilyMember keeps business membership separate from the document's observation head.
// An empty BusinessFamilyID identifies an observed, unassigned document.
type FamilyMember struct {
	BusinessFamilyID          string `json:"business_family_id,omitempty"`
	DocumentID                string `json:"document_id"`
	ObservationChainID        string `json:"observation_chain_id"`
	ObservedHeadVersionNodeID string `json:"observed_head_version_node_id"`
	Availability              string `json:"availability,omitempty"`
}

func (s *Service) family(ctx context.Context, c Command) (Result, error) {
	if s == nil || s.db == nil {
		return Result{}, ErrUnavailable
	}
	if c.Scope.TenantID == 0 || !validID(c.Scope.KnowledgeBaseID) || !validID(c.ActorID) || !validID(c.OperationID) {
		return Result{}, ErrInvalid
	}
	if c.Action == "family_create" {
		label := strings.TrimSpace(c.Payload.FamilyLabel)
		if c.ID != "" || c.ExpectedRevision != 0 || c.Payload.DocumentID != "" || label == "" ||
			!utf8.ValidString(label) || utf8.RuneCountInString(label) > 120 || strings.IndexFunc(label, unicode.IsControl) >= 0 {
			return Result{}, ErrInvalid
		}
		return s.transact(ctx, c, func(tx *sql.Tx) (Result, error) {
			id := newID()
			if id == "" {
				return Result{}, ErrUnavailable
			}
			now := s.now().UTC().Format(time.RFC3339Nano)
			_, err := tx.ExecContext(ctx, `INSERT INTO ext_business_document_families
				(tenant_id,knowledge_base_id,business_family_id,label,revision,created_at,updated_at)
				VALUES(?,?,?,?,1,?,?)`, args(c.Scope, id, label, now, now)...)
			return Result{ID: id, Revision: 1, Status: "created"}, err
		})
	}
	doc := c.Payload.DocumentID
	reason := strings.TrimSpace(c.Payload.Reason)
	if !validID(c.ID) || !validID(doc) || c.ExpectedRevision < 1 || c.Payload.FamilyLabel != "" ||
		len(reason) > 512 || strings.IndexFunc(reason, unicode.IsControl) >= 0 ||
		(c.Action == "family_member_add" && reason != "") {
		return Result{}, ErrInvalid
	}
	if reason == "" {
		reason = "member_removed"
	}
	var version string
	var documents []weknora.AttachmentDocument
	if c.Action == "family_member_add" {
		if receipt, ok, err := s.replay(ctx, c); ok || err != nil {
			return receipt, err
		}
		var eligible bool
		err := s.db.View(ctx, func(tx *sql.Tx) error {
			return tx.QueryRowContext(ctx, `SELECT EXISTS(
				SELECT 1 FROM ext_business_document_families f
				JOIN ext_lifecycle_current c USING(tenant_id,knowledge_base_id)
				WHERE f.tenant_id=? AND f.knowledge_base_id=? AND f.business_family_id=? AND f.revision=?
				AND c.document_id=?)`, args(c.Scope, c.ID, c.ExpectedRevision, doc)...).Scan(&eligible)
		})
		if err != nil {
			return Result{}, err
		}
		if !eligible {
			return s.replayAfterObservationError(ctx, c, ErrConflict)
		}
		// Historical documents are eligible members. Observe identity and scope
		// without requiring the current retrieval permit used by revision actions.
		version, err = s.observeVersion(ctx, c.Scope, doc)
		if err != nil {
			return s.replayAfterObservationError(ctx, c, ErrUnavailable)
		}
		documents, err = s.nativeFamilyPrimaryDocuments(ctx, c.Scope, []string{doc})
		if err != nil {
			return s.replayAfterObservationError(ctx, c, err)
		}
	}
	return s.transact(ctx, c, func(tx *sql.Tx) (Result, error) {
		now := s.now().UTC()
		if err := advanceFamilyRevision(ctx, tx, c.Scope, c.ID, c.ExpectedRevision, now); err != nil {
			return Result{}, err
		}
		out := Result{ID: c.ID, Revision: c.ExpectedRevision + 1, Status: "member_removed"}
		if c.Action == "family_member_add" {
			if err := addFamilyMemberTx(ctx, tx, c.Scope, c.ID, doc, version, documents); err != nil {
				return Result{}, err
			}
			out.Status = "member_added"
		} else {
			update, err := tx.ExecContext(ctx, `DELETE FROM ext_business_document_family_members AS m
				WHERE tenant_id=? AND knowledge_base_id=? AND document_id=? AND business_family_id=?
				AND NOT EXISTS (SELECT 1 FROM ext_business_family_published_heads h
					WHERE h.tenant_id=m.tenant_id AND h.knowledge_base_id=m.knowledge_base_id AND h.business_family_id=m.business_family_id AND h.document_id=m.document_id)
				AND NOT EXISTS (SELECT 1 FROM ext_family_replacements r
					WHERE r.tenant_id=m.tenant_id AND r.knowledge_base_id=m.knowledge_base_id AND r.business_family_id=m.business_family_id
					AND r.status IN ('pending_review','scheduled','needs_review')
					AND (r.old_document_id=m.document_id OR r.new_document_id=m.document_id))`, args(c.Scope, doc, c.ID)...)
			if err := requireFamilyChange(update, err); err != nil {
				return Result{}, err
			}
			update, err = tx.ExecContext(ctx, `UPDATE ext_business_family_member_history SET status='removed',removed_at=?,reason=?
				WHERE tenant_id=? AND knowledge_base_id=? AND business_family_id=? AND document_id=? AND status='active'`,
				append([]any{now.Format(time.RFC3339Nano), reason}, args(c.Scope, c.ID, doc)...)...)
			if err := requireFamilyChange(update, err); err != nil {
				return Result{}, err
			}
		}
		return out, enqueueFamilyProjection(ctx, tx, c, now, []string{doc})
	})
}

func advanceFamilyRevision(ctx context.Context, tx *sql.Tx, scope provenance.Scope, family string, revision int64, now time.Time) error {
	update, err := tx.ExecContext(ctx, `UPDATE ext_business_document_families SET revision=revision+1,updated_at=?
		WHERE tenant_id=? AND knowledge_base_id=? AND business_family_id=? AND revision=?`,
		append([]any{now.Format(time.RFC3339Nano)}, args(scope, family, revision)...)...)
	return requireFamilyChange(update, err)
}

func addFamilyMemberTx(ctx context.Context, tx *sql.Tx, scope provenance.Scope, family, doc, version string, documents []weknora.AttachmentDocument) error {
	if err := requirePrimaryFamilyDocumentTx(ctx, tx, scope, doc); err != nil {
		return err
	}
	for _, document := range documents {
		if err := requireFamilyHeaderTx(ctx, tx, scope, document, version); err != nil {
			return err
		}
	}
	update, err := tx.ExecContext(ctx, `INSERT INTO ext_business_document_family_members
		(tenant_id,knowledge_base_id,document_id,business_family_id)
		SELECT tenant_id,knowledge_base_id,document_id,? FROM ext_document_version_family
		WHERE tenant_id=? AND knowledge_base_id=? AND document_id=? AND current_version_node_id=?
		ON CONFLICT(tenant_id,knowledge_base_id,document_id) DO NOTHING`,
		append([]any{family}, args(scope, doc, version)...)...)
	if err := requireFamilyChange(update, err); err != nil {
		return err
	}
	return recordFamilyMembership(ctx, tx, scope, family, doc)
}

func recordFamilyMembership(ctx context.Context, tx *sql.Tx, scope provenance.Scope, family, doc string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO ext_business_family_member_history
		(tenant_id,knowledge_base_id,business_family_id,document_id,status) VALUES(?,?,?,?,'active')
		ON CONFLICT(tenant_id,knowledge_base_id,business_family_id,document_id)
		DO UPDATE SET status='active',removed_at=NULL,reason=NULL`, args(scope, family, doc)...)
	return err
}

func (s *Service) nativeFamilyPrimaryDocuments(ctx context.Context, scope provenance.Scope, ids []string) ([]weknora.AttachmentDocument, error) {
	native, ok := s.remote.(*NativePublisher)
	if !ok || native == nil || native.client == nil {
		if s.registry == nil {
			return nil, nil
		}
		return nil, ErrUnavailable
	}
	docs, err := native.client.AttachmentDocuments(ctx, []string{scope.KnowledgeBaseID}, ids)
	if err != nil {
		return nil, ErrUnavailable
	}
	if len(docs) != len(ids) {
		return nil, ErrConflict
	}
	for _, doc := range docs {
		if doc.TenantID != scope.TenantID || doc.KnowledgeBaseID != scope.KnowledgeBaseID ||
			doc.IsAttachment || doc.ImportID != "" || doc.ParentDocumentID != "" || doc.ParseStatus != "completed" ||
			strings.HasPrefix(doc.Channel, weknora.SplitPublicationChannelPrefix) {
			return nil, ErrConflict
		}
	}
	return docs, nil
}

func requirePrimaryFamilyDocumentTx(ctx context.Context, tx *sql.Tx, scope provenance.Scope, doc string) error {
	// Ownership is keyed by the child ID. A primary document can own attachments.
	owners, err := store.AttachmentOwnershipsTx(ctx, tx, scope.TenantID, []string{doc})
	if err != nil {
		return err
	}
	if len(owners) != 0 {
		return ErrConflict
	}
	var split bool
	err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM ext_revision_proposals r
		JOIN ext_revision_split_parts p USING(tenant_id,knowledge_base_id,proposal_id)
		WHERE r.tenant_id=? AND r.knowledge_base_id=? AND r.target_document_id=?)`, args(scope, doc)...).Scan(&split)
	if err != nil {
		return err
	}
	if split {
		return ErrConflict
	}
	return nil
}

// requireFamilyHeaderTx detects changes between the pinned source snapshot and
// the separate native metadata read. Native writes do not share this transaction.
func requireFamilyHeaderTx(ctx context.Context, tx *sql.Tx, scope provenance.Scope, doc weknora.AttachmentDocument, version string) error {
	var matches bool
	err := tx.QueryRowContext(ctx, `SELECT COALESCE(
		json_extract(e.snapshot_json,'$.header_fields.parse_status')=? AND
		json_extract(e.snapshot_json,'$.header_fields.enable_status')=? AND
		json_extract(e.snapshot_json,'$.header_fields.channel')=? AND
		json_extract(e.snapshot_json,'$.header_fields.file_hash')=?,0)
		FROM ext_source_snapshot_evidence e JOIN ext_document_version v USING(tenant_id,knowledge_base_id,version_node_id)
		WHERE e.tenant_id=? AND e.knowledge_base_id=? AND v.document_id=? AND v.version_node_id=?`,
		append([]any{digest(doc.ParseStatus), digest(doc.EnableStatus), digest(doc.Channel), digest(doc.FileHash)},
			args(scope, doc.ID, version)...)...).Scan(&matches)
	if errors.Is(err, sql.ErrNoRows) || err == nil && !matches {
		return ErrConflict
	}
	return err
}

func requireFamilyChange(result sql.Result, err error) error {
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return ErrConflict
	}
	return nil
}

func familyPage(c Command) (limit, offset int, err error) {
	page, size := c.Page, c.PageSize
	if page == 0 {
		page = 1
	}
	if size == 0 {
		size = 20
	}
	if c.Scope.TenantID == 0 || !validID(c.Scope.KnowledgeBaseID) || (c.ID != "" && !validID(c.ID)) ||
		page < 1 || page > 10000 || size < 1 || size > 20 {
		return 0, 0, ErrInvalid
	}
	return size, (page - 1) * size, nil
}

// Families lists a page of registered families, optionally restricted to one ID.
func (s *Service) Families(ctx context.Context, c Command) ([]BusinessFamily, error) {
	if s == nil || s.db == nil {
		return nil, ErrUnavailable
	}
	limit, offset, err := familyPage(c)
	if err != nil {
		return nil, err
	}
	items := []BusinessFamily{}
	err = s.db.View(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT f.business_family_id,f.label,f.revision,COALESCE(h.document_id,''),COALESCE(h.version_node_id,''),COALESCE(a.status,'')
			FROM ext_business_document_families f LEFT JOIN ext_business_family_published_heads h USING(tenant_id,knowledge_base_id,business_family_id)
			LEFT JOIN ext_document_availability a ON a.tenant_id=h.tenant_id AND a.knowledge_base_id=h.knowledge_base_id AND a.document_id=h.document_id
			WHERE f.tenant_id=? AND f.knowledge_base_id=? AND (?='' OR f.business_family_id=?)
			ORDER BY f.business_family_id LIMIT ? OFFSET ?`, args(c.Scope, c.ID, c.ID, limit, offset)...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var item BusinessFamily
			if err := rows.Scan(&item.ID, &item.Label, &item.Revision, &item.PublishedDocumentID, &item.PublishedVersionNodeID, &item.PublishedHeadAvailability); err != nil {
				return err
			}
			items = append(items, item)
		}
		return rows.Err()
	})
	return items, err
}

// FamilyMembers lists the scoped members of one family, or observed unassigned documents when ID is empty.
func (s *Service) FamilyMembers(ctx context.Context, c Command) ([]FamilyMember, error) {
	if s == nil || s.db == nil {
		return nil, ErrUnavailable
	}
	limit, offset, err := familyPage(c)
	if err != nil {
		return nil, err
	}
	items := []FamilyMember{}
	if c.ID != "" {
		if err := s.refreshFamilyAvailability(ctx, c.Scope, c.ID); err != nil {
			return nil, err
		}
	}
	err = s.db.View(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT COALESCE(m.business_family_id,''),f.document_id,f.family_id,f.current_version_node_id,COALESCE(a.status,'')
			FROM ext_document_version_family f LEFT JOIN ext_business_document_family_members m USING(tenant_id,knowledge_base_id,document_id)
			LEFT JOIN ext_document_availability a ON a.tenant_id=f.tenant_id AND a.knowledge_base_id=f.knowledge_base_id AND a.document_id=f.document_id
			WHERE f.tenant_id=? AND f.knowledge_base_id=? AND COALESCE(m.business_family_id,'')=?
			ORDER BY f.document_id LIMIT ? OFFSET ?`, args(c.Scope, c.ID, limit, offset)...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var item FamilyMember
			if err := rows.Scan(&item.BusinessFamilyID, &item.DocumentID, &item.ObservationChainID, &item.ObservedHeadVersionNodeID, &item.Availability); err != nil {
				return err
			}
			items = append(items, item)
		}
		return rows.Err()
	})
	return items, err
}
