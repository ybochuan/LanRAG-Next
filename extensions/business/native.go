package business

import (
	"context"
	"errors"
	"github.com/ybochuan/LanRAG-Next/extensions/weknora"
	"net/http"
	"net/url"
	"strconv"
)

// ErrBinarySource preserves Frozen12.8: PDF/DOCX require the user's revised original upload.
var ErrBinarySource = errors.New("binary source requires revised original upload")

func (n *NativePublisher) manualSource(ctx context.Context, p Publication) error {
	if !validID(p.SourceDocumentID) {
		return ErrInvalid
	}
	var out struct {
		Data nativeDocument `json:"data"`
	}
	if e := n.client.JSON(ctx, http.MethodGet, "/api/v1/knowledge/"+url.PathEscape(p.SourceDocumentID), nil, nil, &out); e != nil {
		return e
	}
	if out.Data.ID != p.SourceDocumentID || out.Data.TenantID != p.Scope.TenantID || out.Data.KnowledgeBaseID != p.Scope.KnowledgeBaseID {
		return ErrConflict
	}
	if out.Data.Type != "manual" || out.Data.Metadata.Status != "publish" {
		return ErrBinarySource
	}
	return nil
}

// NativePublisher creates a new Markdown sidecar through the unmodified native API.
type NativePublisher struct{ client *weknora.Client }

// NewNativePublisher reuses the Host's scoped upstream credential.
func NewNativePublisher(c *weknora.Client) *NativePublisher { return &NativePublisher{client: c} }

type nativeDocument struct {
	ID              string `json:"id"`
	TenantID        uint64 `json:"tenant_id"`
	KnowledgeBaseID string `json:"knowledge_base_id"`
	Title           string `json:"title"`
	Type            string `json:"type"`
	ParseStatus     string `json:"parse_status"`
	Metadata        struct {
		Content string `json:"content"`
		Status  string `json:"status"`
	} `json:"metadata"`
}

func marker(p Publication) string { return "AgentRAG revision " + p.Key }
func checkedNative(p Publication, d nativeDocument) (RemoteDocument, error) {
	if !validID(d.ID) || d.TenantID != p.Scope.TenantID || d.KnowledgeBaseID != p.Scope.KnowledgeBaseID || d.Type != "manual" || d.Title != marker(p) || d.Metadata.Status != "publish" || textHash(d.Metadata.Content) != p.ContentHash {
		return RemoteDocument{}, ErrConflict
	}
	return RemoteDocument{ID: d.ID, ContentHash: p.ContentHash, Indexed: d.ParseStatus == "completed"}, nil
}
func (n *NativePublisher) read(ctx context.Context, p Publication, id string) (RemoteDocument, error) {
	var out struct {
		Data nativeDocument `json:"data"`
	}
	if e := n.client.JSON(ctx, http.MethodGet, "/api/v1/knowledge/"+url.PathEscape(id), nil, nil, &out); e != nil {
		return RemoteDocument{}, e
	}
	return checkedNative(p, out.Data)
}

// Reconcile scans complete validated pages and verifies actual remote Markdown.
// A missing/partial page or mismatched marker is unknown, never permission to re-create.
func (n *NativePublisher) Reconcile(ctx context.Context, p Publication) (RemoteDocument, error) {
	if n == nil || n.client == nil || n.client.TenantID() != p.Scope.TenantID || !validID(p.Scope.KnowledgeBaseID) {
		return RemoteDocument{}, ErrUnavailable
	}
	if p.TargetDocumentID != "" {
		return n.read(ctx, p, p.TargetDocumentID)
	}
	if e := n.manualSource(ctx, p); e != nil {
		return RemoteDocument{}, e
	}
	found := RemoteDocument{}
	for page := 1; page != 0; {
		var out struct {
			weknora.Pagination
			Data []nativeDocument `json:"data"`
		}
		q := url.Values{"page": {strconv.Itoa(page)}, "page_size": {"100"}, "sort_by": {"created_at"}, "sort_order": {"asc"}}
		if e := n.client.JSON(ctx, http.MethodGet, "/api/v1/knowledge-bases/"+url.PathEscape(p.Scope.KnowledgeBaseID)+"/knowledge", q, nil, &out); e != nil {
			return RemoteDocument{}, e
		}
		next, e := out.Pagination.Next(page, 100, len(out.Data))
		if e != nil {
			return RemoteDocument{}, e
		}
		for _, d := range out.Data {
			if d.TenantID != p.Scope.TenantID || d.KnowledgeBaseID != p.Scope.KnowledgeBaseID {
				return RemoteDocument{}, ErrConflict
			}
			if d.Title != marker(p) {
				continue
			}
			if found.ID != "" {
				return RemoteDocument{}, ErrConflict
			}
			found, e = n.read(ctx, p, d.ID)
			if e != nil {
				return RemoteDocument{}, e
			}
		}
		page = next
	}
	return found, nil
}

// Create is called once only after a reconciled absence under the persistent Saga lease.
func (n *NativePublisher) Create(ctx context.Context, p Publication) (RemoteDocument, error) {
	if n == nil || n.client == nil || n.client.TenantID() != p.Scope.TenantID || !validID(p.Scope.KnowledgeBaseID) || textHash(p.Content) != p.ContentHash {
		return RemoteDocument{}, ErrInvalid
	}
	body := map[string]any{"title": marker(p), "content": p.Content, "status": "publish", "channel": "api"}
	if e := n.manualSource(ctx, p); e != nil {
		return RemoteDocument{}, e
	}
	var out struct {
		Data nativeDocument `json:"data"`
	}
	if e := n.client.JSON(ctx, http.MethodPost, "/api/v1/knowledge-bases/"+url.PathEscape(p.Scope.KnowledgeBaseID)+"/knowledge/manual", nil, body, &out); e != nil {
		return RemoteDocument{}, e
	}
	return checkedNative(p, out.Data)
}
