package retrieval

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/contract"
	"github.com/ybochuan/LanRAG-Next/extensions/provenance"
	"github.com/ybochuan/LanRAG-Next/extensions/store"
	"github.com/ybochuan/LanRAG-Next/extensions/weknora"
)

type AttachmentRelations interface {
	AttachmentTemplates(context.Context, provenance.Scope, string) ([]string, error)
}

func (p *Policy) WithAttachmentRelations(relations AttachmentRelations) *Policy {
	p.attachmentRelations = relations
	return p
}

// AttachmentSource reads current relationship metadata from the native API.
type AttachmentSource interface {
	AttachmentDocuments(context.Context, []string, []string) ([]weknora.AttachmentDocument, error)
	ListAttachmentDocuments(context.Context, string) ([]weknora.AttachmentDocument, error)
}

// WithAttachments enables native metadata and parent-state checks in worker mode.
func (p *Policy) WithAttachments(s AttachmentSource) *Policy { p.attachments = s; return p }

func (p *Policy) attachmentDocuments(ctx context.Context, r contract.BeforeRetrieveRequest) (map[string]weknora.AttachmentDocument, error) {
	result := map[string]weknora.AttachmentDocument{}
	if p.attachments == nil {
		return nil, nil
	}
	docs, e := p.attachments.AttachmentDocuments(ctx, r.KnowledgeBaseIDs, r.KnowledgeIDs)
	if e != nil {
		return nil, e
	}
	wanted := map[string]bool{}
	for _, id := range r.KnowledgeIDs {
		wanted[id] = true
	}
	for _, d := range docs {
		if !wanted[d.ID] {
			continue
		}
		if d.TenantID != r.TenantID || !slices.Contains(r.KnowledgeBaseIDs, d.KnowledgeBaseID) {
			return nil, errors.New("attachment source scope mismatch")
		}
		if _, ok := result[d.ID]; ok {
			return nil, errors.New("ambiguous attachment identity")
		}
		result[d.ID] = d
	}
	return result, nil
}
func publicationChannelReadable(channel, released string) bool {
	reserved := strings.HasPrefix(channel, weknora.SplitPublicationChannelPrefix) || strings.HasPrefix(channel, weknora.RevisionPublicationChannelPrefix)
	return !reserved || channel == released
}

func attachmentParentActive(ctx context.Context, tx *sql.Tx, tenant uint64, parent weknora.AttachmentDocument, now time.Time) (bool, error) {
	var state, publishedChannel string
	var from, until sql.NullString
	e := tx.QueryRowContext(ctx, `SELECT CASE WHEN `+store.LifecycleReadableSQL+` AND NOT `+store.PublicationTargetPendingSQL+` THEN c.state ELSE '' END,
	COALESCE(`+store.PublishedPublicationChannelSQL+`,''),a.effective_from,a.effective_until FROM ext_lifecycle_current c `+store.LifecycleEventJoinSQL+store.LifecycleAnnotationJoinSQL+`
	WHERE c.tenant_id=? AND c.knowledge_base_id=? AND c.document_id=?
	AND NOT EXISTS (SELECT 1 FROM ext_attachment_delete_intents d WHERE d.tenant_id=c.tenant_id
	AND d.knowledge_base_id=c.knowledge_base_id AND d.document_id=c.document_id)`, fmt.Sprint(tenant), parent.KnowledgeBaseID, parent.ID).Scan(&state, &publishedChannel, &from, &until)
	effective, _ := store.EffectiveAt(now, from, until)
	if errors.Is(e, sql.ErrNoRows) {
		return false, nil
	}
	if e != nil || state != "active" || !effective || !publicationChannelReadable(parent.Channel, publishedChannel) {
		return false, e
	}
	held, e := store.LegacyPublicationHeldTx(ctx, tx, parent)
	return !held && e == nil, e
}

// attachmentParents extends only the internal policy lookup; parents are never
// added to the caller's authorized retrieval set.
func (p *Policy) attachmentParents(ctx context.Context, r contract.BeforeRetrieveRequest, docs map[string]weknora.AttachmentDocument) (map[string]weknora.AttachmentDocument, error) {
	parents := make(map[string]weknora.AttachmentDocument)
	missing := make(map[string]bool)
	for _, d := range docs {
		if !d.IsAttachment {
			continue
		}
		if parent, ok := docs[d.ParentDocumentID]; ok {
			parents[parent.ID] = parent
		} else {
			missing[d.ParentDocumentID] = true
		}
	}
	if len(missing) == 0 {
		return parents, nil
	}
	ids := make([]string, 0, len(missing))
	for id := range missing {
		ids = append(ids, id)
	}
	found, err := p.attachments.AttachmentDocuments(ctx, r.KnowledgeBaseIDs, ids)
	if err != nil {
		return nil, err
	}
	for _, parent := range found {
		if missing[parent.ID] {
			parents[parent.ID] = parent
		}
	}
	return parents, nil
}

func validAttachmentParent(child, parent weknora.AttachmentDocument) bool {
	return parent.ID != "" && parent.ID == child.ParentDocumentID && parent.TenantID == child.TenantID &&
		parent.KnowledgeBaseID == child.KnowledgeBaseID && !parent.IsAttachment &&
		(parent.EnableStatus == "" || parent.EnableStatus == "enabled") && (parent.ParseStatus == "" || parent.ParseStatus == "completed")
}

// AttachmentLinks enriches parent hits and direct attachment hits with current permits.
func (p *Policy) AttachmentLinks(ctx context.Context, r contract.BeforeRetrieveRequest) (map[string][]contract.RelatedAttachment, error) {
	if r.Validate() != nil || r.TenantID != p.tenant || p.attachments == nil {
		return nil, errors.New("attachment lookup unavailable")
	}
	hits := map[string]bool{}
	for _, id := range r.KnowledgeIDs {
		hits[id] = true
	}
	for _, kb := range r.KnowledgeBaseIDs {
		if !p.kbs[kb] {
			return nil, errors.New("attachment KB denied")
		}
	}
	result := map[string][]contract.RelatedAttachment{}
	matches := map[string][]weknora.AttachmentDocument{}
	ids := []string{}
	seen := map[string]bool{}
	for _, kb := range r.KnowledgeBaseIDs {
		docs, e := p.attachments.ListAttachmentDocuments(ctx, kb)
		if e != nil {
			return nil, e
		}
		for _, d := range docs {
			if d.TenantID != r.TenantID || d.KnowledgeBaseID != kb {
				return nil, errors.New("attachment lookup scope mismatch")
			}
			if !d.IsAttachment || (!hits[d.ID] && !hits[d.ParentDocumentID]) {
				continue
			}
			if !seen[d.ID] {
				ids = append(ids, d.ID)
				seen[d.ID] = true
			}
			if hits[d.ID] {
				matches[d.ID] = append(matches[d.ID], d)
			}
			if hits[d.ParentDocumentID] {
				matches[d.ParentDocumentID] = append(matches[d.ParentDocumentID], d)
			}
		}
	}
	if p.attachmentRelations != nil {
		if err := p.appendTemplateMatches(ctx, r, matches, seen, &ids); err != nil {
			return nil, err
		}
	}
	if len(ids) == 0 {
		return result, nil
	}
	check := r
	check.KnowledgeIDs = ids
	excluded, e := p.ExcludedDocumentIDs(ctx, check)
	if e != nil {
		return nil, e
	}
	blocked := map[string]bool{}
	for _, id := range excluded {
		blocked[id] = true
	}
	semantics, e := p.attachmentSemantics(ctx, ids)
	if e != nil {
		return nil, e
	}
	var confirmed []store.AttachmentConfirmation
	if p.db != nil {
		confirmed, e = p.db.AttachmentConfirmations(ctx, r.TenantID, ids)
		if e != nil {
			return nil, e
		}
	}
	parentHashes, parentTitles, parentMetaOK := p.attachmentParentMeta(ctx, matches, r.KnowledgeBaseIDs)
	if !parentMetaOK {
		semantics = map[string]store.AttachmentSemantic{}
	}
	for hit, docs := range matches {
		for _, d := range docs {
			if blocked[d.ID] || d.FileName == "" {
				continue
			}
			if len(result[hit]) >= 32 {
				break
			}
			item := contract.RelatedAttachment{KnowledgeID: d.ID, KnowledgeBaseID: d.KnowledgeBaseID, ParentDocumentID: d.ParentDocumentID, FileName: d.FileName, DownloadURL: "/api/v1/knowledge/" + url.PathEscape(d.ID) + "/download"}
			if semantic, ok := semantics[d.ID]; ok && semantic.KnowledgeBaseID == d.KnowledgeBaseID {
				// A changed parent file hash means the semantic mapping belongs to
				// an older version and must not leak into the new version.
				if parentHash := parentHashes[d.ParentDocumentID]; parentHash != "" && semantic.ParentFileHash == parentHash {
					item.DisplayName = semantic.DisplayName
					item.OriginalFilename = semantic.OriginalFilename
					item.AttachmentSequence = semantic.AttachmentSequence
					item.ReferenceTitle = semantic.ReferenceTitle
					item.ReferenceLocation = semantic.ReferenceLocation
					item.ReferenceExcerpt = semantic.ReferenceExcerpt
					item.MappingStatus = semantic.MappingStatus
					item.MappingConfidence = semantic.MappingConfidence
					item.IndexPolicy = semantic.IndexPolicy
					item.ParentTitle = semantic.ParentTitle
				}
			}
			if item.ParentTitle == "" {
				item.ParentTitle = parentTitles[d.ParentDocumentID]
			}
			if d.Title != "" && !strings.EqualFold(d.Title, d.FileName) {
				item.DisplayName = d.Title
			}
			for _, decision := range confirmed {
				if decision.AttachmentID == d.ID && decision.KnowledgeBaseID == d.KnowledgeBaseID &&
					decision.ParentID == d.ParentDocumentID && decision.ParentHash == parentHashes[d.ParentDocumentID] &&
					decision.AttachmentHash == d.FileHash {
					item.MappingStatus = "manual"
					item.ReferenceTitle = decision.Title
					item.ReferenceLocation = decision.Location
					item.ReferenceExcerpt = decision.Excerpt
					item.AttachmentSequence = decision.Sequence
					item.MappingConfidence = 0
					break
				}
			}
			if !d.IsAttachment && item.MappingStatus == "" {
				item.MappingStatus = "template"
			}
			result[hit] = append(result[hit], item)
		}
	}
	for hit := range result {
		sort.SliceStable(result[hit], func(i, j int) bool {
			left, right := result[hit][i], result[hit][j]
			leftSequence, rightSequence := left.AttachmentSequence, right.AttachmentSequence
			if leftSequence <= 0 {
				leftSequence = int(^uint(0) >> 1)
			}
			if rightSequence <= 0 {
				rightSequence = int(^uint(0) >> 1)
			}
			if leftSequence != rightSequence {
				return leftSequence < rightSequence
			}
			if left.OriginalFilename != right.OriginalFilename {
				return left.OriginalFilename < right.OriginalFilename
			}
			return left.FileName < right.FileName
		})
	}
	return result, nil
}

func (p *Policy) appendTemplateMatches(ctx context.Context, r contract.BeforeRetrieveRequest, matches map[string][]weknora.AttachmentDocument, seen map[string]bool, ids *[]string) error {
	keys, _ := json.Marshal(r.KnowledgeIDs)
	kbs, _ := json.Marshal(r.KnowledgeBaseIDs)
	type parentScope struct{ kb, id string }
	var parents []parentScope
	if err := p.db.View(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT DISTINCT target_knowledge_base_id,target_document_id FROM ext_document_relations
			WHERE tenant_id=? AND edge_type='template_for' AND status='active'
			AND target_document_id IN (SELECT value FROM json_each(?)) AND target_knowledge_base_id IN (SELECT value FROM json_each(?))
			ORDER BY target_knowledge_base_id,target_document_id`,
			fmt.Sprint(r.TenantID), string(keys), string(kbs))
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var parent parentScope
			if err := rows.Scan(&parent.kb, &parent.id); err != nil {
				return err
			}
			parents = append(parents, parent)
		}
		return rows.Err()
	}); err != nil {
		return err
	}
	for _, parent := range parents {
		templates, err := p.attachmentRelations.AttachmentTemplates(ctx, provenance.Scope{TenantID: r.TenantID, KnowledgeBaseID: parent.kb}, parent.id)
		if err != nil {
			return err
		}
		templates = templates[:min(len(templates), 32)]
		docs, err := p.attachments.AttachmentDocuments(ctx, []string{parent.kb}, templates)
		if err != nil {
			return err
		}
		for _, doc := range docs {
			if doc.IsAttachment || doc.KnowledgeBaseID != parent.kb || doc.TenantID != r.TenantID {
				continue
			}
			doc.ParentDocumentID = parent.id
			matches[parent.id] = append(matches[parent.id], doc)
			if !seen[doc.ID] {
				seen[doc.ID] = true
				*ids = append(*ids, doc.ID)
			}
		}
	}
	return nil
}

func (p *Policy) attachmentSemantics(ctx context.Context, ids []string) (map[string]store.AttachmentSemantic, error) {
	if p.db == nil {
		return map[string]store.AttachmentSemantic{}, nil
	}
	return p.db.AttachmentSemantics(ctx, p.tenant, ids)
}

func (p *Policy) attachmentParentMeta(ctx context.Context, matches map[string][]weknora.AttachmentDocument, kbs []string) (map[string]string, map[string]string, bool) {
	hashes := map[string]string{}
	titles := map[string]string{}
	parents := []string{}
	seen := map[string]bool{}
	for _, docs := range matches {
		for _, d := range docs {
			if d.ParentDocumentID == "" || seen[d.ParentDocumentID] {
				continue
			}
			seen[d.ParentDocumentID] = true
			parents = append(parents, d.ParentDocumentID)
		}
	}
	if len(parents) == 0 {
		return hashes, titles, true
	}
	docs, err := p.attachments.AttachmentDocuments(ctx, kbs, parents)
	if err != nil {
		return hashes, titles, false
	}
	for _, d := range docs {
		hashes[d.ID] = d.FileHash
		titles[d.ID] = d.Title
	}
	return hashes, titles, true
}
