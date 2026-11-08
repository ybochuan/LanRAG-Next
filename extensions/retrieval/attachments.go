package retrieval

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/ybochuan/LanRAG-Next/extensions/contract"
	"github.com/ybochuan/LanRAG-Next/extensions/store"
	"github.com/ybochuan/LanRAG-Next/extensions/weknora"
	"net/url"
	"sort"
)

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
		if d.TenantID != r.TenantID || !p.kbs[d.KnowledgeBaseID] {
			return nil, errors.New("attachment source scope mismatch")
		}
		if _, ok := result[d.ID]; ok {
			return nil, errors.New("ambiguous attachment identity")
		}
		result[d.ID] = d
	}
	return result, nil
}
func attachmentParentActive(ctx context.Context, tx *sql.Tx, tenant uint64, d weknora.AttachmentDocument) (bool, error) {
	if !weknora.ValidID(d.ParentDocumentID) || d.ParentDocumentID == d.ID {
		return false, nil
	}
	var state string
	e := tx.QueryRowContext(ctx, `SELECT CASE WHEN `+store.LifecycleReadableSQL+` THEN c.state ELSE '' END FROM ext_lifecycle_current c `+store.LifecycleEventJoinSQL+`
	WHERE c.tenant_id=? AND c.knowledge_base_id=? AND c.document_id=?
	AND NOT EXISTS (SELECT 1 FROM ext_attachment_delete_intents d WHERE d.tenant_id=c.tenant_id
	AND d.knowledge_base_id=c.knowledge_base_id AND d.document_id=c.document_id)`, fmt.Sprint(tenant), d.KnowledgeBaseID, d.ParentDocumentID).Scan(&state)
	if errors.Is(e, sql.ErrNoRows) {
		return false, nil
	}
	return state == "active", e
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
		parent.EnableStatus != "disabled" && (parent.ParseStatus == "" || parent.ParseStatus == "completed")
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
				if parentHash := parentHashes[d.ParentDocumentID]; parentHash == "" || semantic.ParentFileHash == parentHash {
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
