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
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"time"
)

// RelatedAttachment is a native document download descriptor, not a capability URL.
type RelatedAttachment struct {
	KnowledgeID        string  `json:"knowledge_id"`
	KnowledgeBaseID    string  `json:"knowledge_base_id"`
	ParentDocumentID   string  `json:"parent_document_id"`
	FileName           string  `json:"file_name"`
	DownloadURL        string  `json:"download_url"`
	DisplayName        string  `json:"display_name,omitempty"`
	OriginalFilename   string  `json:"original_filename,omitempty"`
	AttachmentSequence int     `json:"attachment_sequence,omitempty"`
	ReferenceTitle     string  `json:"reference_title,omitempty"`
	ReferenceLocation  string  `json:"reference_location,omitempty"`
	ReferenceExcerpt   string  `json:"reference_excerpt,omitempty"`
	MappingStatus      string  `json:"mapping_status,omitempty"`
	MappingConfidence  float64 `json:"mapping_confidence,omitempty"`
	ParentTitle        string  `json:"parent_title,omitempty"`
	IndexPolicy        string  `json:"index_policy,omitempty"`
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

// StoredDocumentID recognizes the tenant/document/file layout used by native
// uploads. Derived exports have a separate namespace. Clean local path aliases
// before matching so they cannot evade the document lookup.
func StoredDocumentID(tenantID uint64, storagePath string) string {
	parts := strings.Split(filepath.ToSlash(filepath.Clean(storagePath)), "/")
	n := len(parts)
	if n < 3 || parts[n-3] != strconv.FormatUint(tenantID, 10) {
		return ""
	}
	id, err := uuid.Parse(parts[n-2])
	if err != nil {
		return ""
	}
	return id.String()
}

// IsAttachmentDocument identifies native documents subject to the attachment permit.
func IsAttachmentDocument(k *types.Knowledge) bool {
	return k != nil && k.GetMetadata()["is_attachment"] == "true"
}

// GuardAttachmentDownload keeps the original native authorization and checks
// the Host permit for an attachment before opening its file.
func GuardAttachmentDownload(ctx context.Context, k *types.Knowledge) error {
	if k == nil {
		return ErrDocumentUnavailable
	}
	return GuardAttachmentDownloads(ctx, k.TenantID, k.KnowledgeBaseID, []*types.Knowledge{k})
}

// GuardAttachmentDownloads checks the attachments in an authorized single-KB
// selection in one request, before any caller begins streaming bytes.
func GuardAttachmentDownloads(ctx context.Context, tenantID uint64, kbID string, documents []*types.Knowledge) error {
	var ids []string
	for _, k := range documents {
		if k == nil || k.TenantID != tenantID || k.KnowledgeBaseID != kbID {
			return ErrDocumentUnavailable
		}
		if IsAttachmentDocument(k) {
			ids = append(ids, k.ID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	allowed, err := AllowedDocuments(ctx, tenantID, kbID, ids)
	if err != nil {
		return ErrDocumentUnavailable
	}
	for _, id := range ids {
		if !allowed[id] {
			return ErrDocumentUnavailable
		}
	}
	return nil
}
