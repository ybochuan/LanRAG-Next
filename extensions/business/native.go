package business

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"

	"github.com/ybochuan/LanRAG-Next/extensions/provenance"
	"github.com/ybochuan/LanRAG-Next/extensions/weknora"
)

// ErrBinarySource preserves Frozen12.8: PDF/DOCX require the user's revised original upload.
var ErrBinarySource = errors.New("binary source requires revised original upload")

func (n *NativePublisher) manualSource(ctx context.Context, p Publication) error {
	if n == nil || n.client == nil || n.client.TenantID() != p.Scope.TenantID || !validID(p.Scope.KnowledgeBaseID) {
		return ErrUnavailable
	}
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
	var meta manualMetadata
	if out.Data.Type != "manual" || json.Unmarshal(out.Data.Metadata, &meta) != nil || meta.Status != "publish" {
		return ErrBinarySource
	}
	return nil
}

// NativePublisher creates a new Markdown sidecar through the unmodified native API.
type NativePublisher struct{ client *weknora.Client }

// NewNativePublisher reuses the Host's scoped upstream credential.
func NewNativePublisher(c *weknora.Client) *NativePublisher { return &NativePublisher{client: c} }

type nativeDocument struct {
	ID              string          `json:"id"`
	TenantID        uint64          `json:"tenant_id"`
	KnowledgeBaseID string          `json:"knowledge_base_id"`
	Title           string          `json:"title"`
	Type            string          `json:"type"`
	Channel         string          `json:"channel"`
	ParseStatus     string          `json:"parse_status"`
	Metadata        json.RawMessage `json:"metadata"`
}

type manualMetadata struct {
	Content string `json:"content"`
	Status  string `json:"status"`
}

func marker(p Publication) string {
	if p.SplitID != "" {
		return p.SplitTitle
	}
	return weknora.RevisionPublicationTitlePrefix + p.Key
}

func publicationChannel(p Publication) string {
	if p.SplitID != "" {
		return weknora.SplitPublicationChannelPrefix + p.ProposalID
	}
	return weknora.RevisionPublicationChannelPrefix + p.ProposalID
}

// Pre-upgrade ordinary publications used the deterministic title and api channel.
func legacyPublication(p Publication, d nativeDocument) bool {
	return p.SplitID == "" && d.Channel == "api" && d.Title == marker(p)
}

func checkedNative(p Publication, d nativeDocument) (RemoteDocument, error) {
	var meta manualMetadata
	if !validID(d.ID) || d.TenantID != p.Scope.TenantID || d.KnowledgeBaseID != p.Scope.KnowledgeBaseID ||
		d.Type != "manual" || d.Title != marker(p) || json.Unmarshal(d.Metadata, &meta) != nil || textHash(meta.Content) != p.ContentHash {
		return RemoteDocument{}, ErrConflict
	}
	if d.Channel != publicationChannel(p) && !legacyPublication(p, d) {
		return RemoteDocument{}, ErrConflict
	}
	draft := meta.Status == "draft" && d.ParseStatus == "draft" && d.Channel == publicationChannel(p)
	if !draft && meta.Status != "publish" {
		return RemoteDocument{}, ErrConflict
	}
	// The native snapshot hashes the metadata bytes as a JSON-encoded byte slice.
	metadata, _ := json.Marshal([]byte(d.Metadata))
	return RemoteDocument{ID: d.ID, ContentHash: p.ContentHash, Draft: draft,
		Indexed: !draft && d.ParseStatus == "completed", IndexFailed: !draft && d.ParseStatus == "failed",
		MetadataHash: textHash(string(metadata))}, nil
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
			if d.Channel != publicationChannel(p) && !legacyPublication(p, d) {
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

// ValidateSplitSource verifies the native source can enter the Markdown split flow.
func (n *NativePublisher) ValidateSplitSource(ctx context.Context, p Publication) error {
	return n.manualSource(ctx, p)
}

func (n *NativePublisher) repealDocumentInactive(ctx context.Context, scope provenance.Scope, id string) (bool, error) {
	if n == nil || n.client == nil || n.client.TenantID() != scope.TenantID {
		return false, ErrUnavailable
	}
	docs, err := n.client.AttachmentDocuments(ctx, []string{scope.KnowledgeBaseID}, []string{id})
	if err != nil {
		return false, err
	}
	if len(docs) == 0 {
		return true, nil
	}
	doc := docs[0]
	switch doc.ParseStatus {
	case "draft", "failed", "deleting", "cancelled":
		return true, nil
	case "completed":
		return doc.EnableStatus == "disabled", nil
	default:
		return false, nil
	}
}

// CreateDraft creates a natively disabled document before the local target binding.
func (n *NativePublisher) CreateDraft(ctx context.Context, p Publication) (RemoteDocument, error) {
	if !validID(p.ProposalID) || p.TargetDocumentID != "" {
		return RemoteDocument{}, ErrInvalid
	}
	if err := n.manualSource(ctx, p); err != nil {
		return RemoteDocument{}, err
	}
	return n.writeManual(ctx, p, http.MethodPost, "/api/v1/knowledge-bases/"+url.PathEscape(p.Scope.KnowledgeBaseID)+"/knowledge/manual", "draft")
}

// IndexDraft asks Core to index the same target, after its gate has been committed.
func (n *NativePublisher) IndexDraft(ctx context.Context, p Publication) (RemoteDocument, error) {
	if !validID(p.ProposalID) || !validID(p.TargetDocumentID) {
		return RemoteDocument{}, ErrInvalid
	}
	return n.writeManual(ctx, p, http.MethodPut, "/api/v1/knowledge/manual/"+url.PathEscape(p.TargetDocumentID), "publish")
}

func (n *NativePublisher) writeManual(ctx context.Context, p Publication, method, route, status string) (RemoteDocument, error) {
	if n == nil || n.client == nil || n.client.TenantID() != p.Scope.TenantID ||
		!validID(p.Scope.KnowledgeBaseID) || textHash(p.Content) != p.ContentHash {
		return RemoteDocument{}, ErrInvalid
	}
	body := map[string]any{"title": marker(p), "content": p.Content, "status": status, "channel": publicationChannel(p)}
	var out struct {
		Data nativeDocument `json:"data"`
	}
	if e := n.client.JSON(ctx, method, route, nil, body, &out); e != nil {
		return RemoteDocument{}, e
	}
	return checkedNative(p, out.Data)
}
