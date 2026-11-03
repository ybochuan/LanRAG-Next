package extensionbridge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"io"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"time"
)

// RelatedAttachment is a native document download descriptor, not a capability URL.
type RelatedAttachment struct {
	KnowledgeID      string `json:"knowledge_id"`
	KnowledgeBaseID  string `json:"knowledge_base_id"`
	ParentDocumentID string `json:"parent_document_id"`
	FileName         string `json:"file_name"`
	DownloadURL      string `json:"download_url"`
}
type attachmentLookup struct {
	RequestID        string   `json:"request_id"`
	SessionID        string   `json:"session_id"`
	Mode             string   `json:"mode"`
	TenantID         uint64   `json:"tenant_id"`
	KnowledgeBaseIDs []string `json:"knowledge_base_ids"`
	KnowledgeIDs     []string `json:"knowledge_ids"`
}

func attachmentLinksHTTP(ctx context.Context, addr, key string, in attachmentLookup) (map[string][]RelatedAttachment, error) {
	body, e := json.Marshal(in)
	if e != nil {
		return nil, e
	}
	request, e := http.NewRequestWithContext(ctx, "POST", addr, bytes.NewReader(body))
	if e != nil {
		return nil, e
	}
	request.Header.Set("Authorization", "Bearer "+key)
	request.Header.Set("X-AgentRAG-Bridge-Version", "1")
	request.Header.Set("Content-Type", "application/json")
	client := *bridgeHTTPClient
	client.Timeout = 2 * time.Second
	response, e := client.Do(request)
	if e != nil {
		return nil, e
	}
	defer response.Body.Close()
	raw, e := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	if e != nil || len(raw) > 1<<20 || response.StatusCode != 200 {
		return nil, errors.New("attachment metadata unavailable")
	}
	var out struct {
		RequestID        string                         `json:"request_id"`
		TenantID         uint64                         `json:"tenant_id"`
		KnowledgeBaseIDs []string                       `json:"knowledge_base_ids"`
		Links            map[string][]RelatedAttachment `json:"related_attachments"`
	}
	if json.Unmarshal(raw, &out) != nil || out.RequestID != in.RequestID || out.TenantID != in.TenantID || !reflect.DeepEqual(out.KnowledgeBaseIDs, in.KnowledgeBaseIDs) {
		return nil, errors.New("attachment response scope mismatch")
	}
	hits := map[string]bool{}
	kbs := map[string]bool{}
	for _, id := range in.KnowledgeIDs {
		hits[id] = true
	}
	for _, id := range in.KnowledgeBaseIDs {
		kbs[id] = true
	}
	for hit, links := range out.Links {
		if !hits[hit] || len(links) > 32 {
			return nil, errors.New("attachment response outside hit scope")
		}
		for _, a := range links {
			if !kbs[a.KnowledgeBaseID] || a.KnowledgeID == "" || a.ParentDocumentID == "" || a.FileName == "" || (hit != a.KnowledgeID && hit != a.ParentDocumentID) || a.DownloadURL != "/api/v1/knowledge/"+url.PathEscape(a.KnowledgeID)+"/download" {
				return nil, errors.New("invalid attachment descriptor")
			}
		}
	}
	return out.Links, nil
}

// EnrichAttachmentResults adds optional metadata after governance and retrieval.
// Enrichment is fail-open for the answer, but stale/failed metadata is never kept.
func EnrichAttachmentResults(ctx context.Context, tenant uint64, kbs []string, results []*types.SearchResult) {
	ids := []string{}
	seen := map[string]bool{}
	for _, r := range results {
		if r == nil {
			continue
		}
		copy := map[string]string{}
		for k, v := range r.Metadata {
			if k != "related_attachments" {
				copy[k] = v
			}
		}
		r.Metadata = copy
		if r.KnowledgeID != "" && !seen[r.KnowledgeID] && r.MatchType != types.MatchTypeWebSearch {
			ids = append(ids, r.KnowledgeID)
			seen[r.KnowledgeID] = true
		}
	}
	key := os.Getenv("AGENTRAG_MANAGEMENT_KEY")
	if len(ids) == 0 || len(kbs) == 0 || len(key) < 32 {
		return
	}
	trace := uuid.NewString()
	in := attachmentLookup{trace, trace, "rag", tenant, kbs, ids}
	links, e := attachmentLinksHTTP(ctx, "http://127.0.0.1:8090/bridge/after-retrieve", key, in)
	if e != nil {
		return
	}
	for _, r := range results {
		if r == nil || len(links[r.KnowledgeID]) == 0 {
			continue
		}
		b, e := json.Marshal(links[r.KnowledgeID])
		if e == nil {
			r.Metadata["related_attachments"] = string(b)
		}
	}
}

// GuardAttachmentDownload keeps the original native download authorization and
// reuses the Host permit at click time for attachment documents only.
func GuardAttachmentDownload(ctx context.Context, k *types.Knowledge) error {
	if k == nil {
		return ErrDocumentUnavailable
	}
	if k.GetMetadata()["is_attachment"] != "true" {
		return nil
	}
	allowed, e := AllowedDocuments(ctx, k.TenantID, k.KnowledgeBaseID, []string{k.ID})
	if e != nil || !allowed[k.ID] {
		return ErrDocumentUnavailable
	}
	return nil
}
