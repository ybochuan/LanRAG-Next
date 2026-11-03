package weknora

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strconv"
)

// AttachmentDocument is the minimal native metadata used by attachment policy.
type AttachmentDocument struct {
	ID               string
	TenantID         uint64
	KnowledgeBaseID  string
	FileName         string
	IsAttachment     bool
	ParentDocumentID string
}
type nativeAttachmentDocument struct {
	attachmentDocument
	FileName string `json:"file_name"`
}

func (c *Client) decodeAttachmentDocument(d nativeAttachmentDocument, kbs map[string]bool) (AttachmentDocument, error) {
	if !ValidID(d.ID) || d.TenantID != c.tenant || !kbs[d.KBID] {
		return AttachmentDocument{}, errors.New("attachment metadata scope mismatch")
	}
	var flag string
	if raw, ok := d.Metadata["is_attachment"]; ok && json.Unmarshal(raw, &flag) != nil {
		return AttachmentDocument{}, errors.New("invalid attachment metadata")
	}
	out := AttachmentDocument{ID: d.ID, TenantID: d.TenantID, KnowledgeBaseID: d.KBID, FileName: d.FileName, IsAttachment: flag == "true", ParentDocumentID: d.value("parent_document_id")}
	if out.IsAttachment && (!ValidID(out.ParentDocumentID) || out.ParentDocumentID == out.ID) {
		return AttachmentDocument{}, errors.New("invalid attachment parent")
	}
	return out, nil
}

// AttachmentDocuments fetches current metadata in bounded native batch requests.
func (c *Client) AttachmentDocuments(ctx context.Context, kbs, ids []string) ([]AttachmentDocument, error) {
	allowed := map[string]bool{}
	for _, kb := range kbs {
		if !ValidID(kb) {
			return nil, errors.New("invalid attachment KB")
		}
		allowed[kb] = true
	}
	result := make([]AttachmentDocument, 0, len(ids))
	seen := map[string]bool{}
	for offset := 0; offset < len(ids); offset += 100 {
		batch := ids[offset:min(offset+100, len(ids))]
		wanted := map[string]bool{}
		for _, id := range batch {
			if !ValidID(id) {
				return nil, errors.New("invalid attachment identity")
			}
			wanted[id] = true
		}
		var out struct {
			Data []nativeAttachmentDocument `json:"data"`
		}
		if e := c.JSON(ctx, "GET", "/api/v1/knowledge/batch", url.Values{"ids": batch}, nil, &out); e != nil {
			return nil, e
		}
		for _, d := range out.Data {
			if !wanted[d.ID] || seen[d.ID] {
				return nil, errors.New("attachment batch identity mismatch")
			}
			v, e := c.decodeAttachmentDocument(d, allowed)
			if e != nil {
				return nil, e
			}
			seen[d.ID] = true
			result = append(result, v)
		}
	}
	return result, nil
}

// ListAttachmentDocuments reads the native KB list without changing any records.
func (c *Client) ListAttachmentDocuments(ctx context.Context, kb string) ([]AttachmentDocument, error) {
	if !ValidID(kb) {
		return nil, errors.New("invalid attachment KB")
	}
	result := []AttachmentDocument{}
	seen := map[string]bool{}
	for page := 1; page != 0; {
		var out struct {
			Pagination
			Data []nativeAttachmentDocument `json:"data"`
		}
		if e := c.JSON(ctx, "GET", "/api/v1/knowledge-bases/"+url.PathEscape(kb)+"/knowledge", url.Values{"page": {strconv.Itoa(page)}, "page_size": {"100"}}, nil, &out); e != nil {
			return nil, e
		}
		next, e := out.Pagination.Next(page, 100, len(out.Data))
		if e != nil {
			return nil, e
		}
		for _, d := range out.Data {
			v, e := c.decodeAttachmentDocument(d, map[string]bool{kb: true})
			if e != nil {
				return nil, e
			}
			if seen[v.ID] {
				return nil, errors.New("duplicate attachment list identity")
			}
			seen[v.ID] = true
			if v.IsAttachment {
				result = append(result, v)
			}
		}
		page = next
	}
	return result, nil
}
