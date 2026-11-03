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
	e := tx.QueryRowContext(ctx, `SELECT CASE WHEN `+store.LifecycleCurrentVerifiedSQL+` THEN c.state ELSE '' END FROM ext_lifecycle_current c `+store.LifecycleEventJoinSQL+` WHERE c.tenant_id=? AND c.knowledge_base_id=? AND c.document_id=?`, fmt.Sprint(tenant), d.KnowledgeBaseID, d.ParentDocumentID).Scan(&state)
	if errors.Is(e, sql.ErrNoRows) {
		return false, nil
	}
	return state == "active", e
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
	for hit, docs := range matches {
		for _, d := range docs {
			if blocked[d.ID] || d.FileName == "" {
				continue
			}
			if len(result[hit]) >= 32 {
				break
			}
			result[hit] = append(result[hit], contract.RelatedAttachment{KnowledgeID: d.ID, KnowledgeBaseID: d.KnowledgeBaseID, ParentDocumentID: d.ParentDocumentID, FileName: d.FileName, DownloadURL: "/api/v1/knowledge/" + url.PathEscape(d.ID) + "/download"})
		}
	}
	return result, nil
}
