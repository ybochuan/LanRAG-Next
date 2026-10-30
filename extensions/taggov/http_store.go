package taggov

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"

	"github.com/ybochuan/LanRAG-Next/extensions/weknora"
)

// ErrAmbiguousTags protects business tags when native tag loading was inconclusive.
var ErrAmbiguousTags = errors.New("native tags missing or null; full replacement refused")

// HTTPStore reads/replaces tags through the authenticated native public API.
type HTTPStore struct{ client *weknora.Client }

// NewHTTPStore shares a tenant-bound client; it never creates native tags.
func NewHTTPStore(client *weknora.Client) *HTTPStore { return &HTTPStore{client} }

func (s *HTTPStore) validScope(scope Scope) bool {
	return s != nil && s.client != nil && scope.TenantID == s.client.TenantID() && validScope(scope)
}

func validScope(scope Scope) bool {
	return scope.TenantID != 0 && weknora.ValidID(scope.KnowledgeBaseID) && weknora.ValidID(scope.DocumentID)
}

type nativeTag struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	TenantID uint64 `json:"tenant_id"`
	KBID     string `json:"knowledge_base_id"`
}

func decodeTags(tags []nativeTag, scope Scope) ([]Tag, error) {
	result := make([]Tag, 0, len(tags))
	seen := make(map[string]bool)
	for _, tag := range tags {
		if !weknora.ValidID(tag.ID) || tag.Name == "" || tag.TenantID != scope.TenantID || tag.KBID != scope.KnowledgeBaseID || seen[tag.ID] {
			return nil, errors.New("invalid native tag identity or scope")
		}
		seen[tag.ID] = true
		result = append(result, Tag{tag.ID, tag.Name})
	}
	return result, nil
}

// ReadTags rejects null/missing tags: the upstream API returns success even when
// its tag lookup failed, making null indistinguishable from a truly untagged doc.
func (s *HTTPStore) ReadTags(ctx context.Context, scope Scope) ([]Tag, error) {
	if !s.validScope(scope) {
		return nil, errors.New("invalid native tag scope")
	}
	var envelope struct {
		Data *struct {
			ID       string      `json:"id"`
			TenantID uint64      `json:"tenant_id"`
			KBID     string      `json:"knowledge_base_id"`
			Tags     []nativeTag `json:"tags"`
		} `json:"data"`
	}
	if err := s.client.JSON(ctx, http.MethodGet, "/api/v1/knowledge/"+url.PathEscape(scope.DocumentID), nil, nil, &envelope); err != nil {
		return nil, err
	}
	doc := envelope.Data
	if doc == nil || doc.ID != scope.DocumentID || doc.TenantID != scope.TenantID || doc.KBID != scope.KnowledgeBaseID {
		return nil, errors.New("native document scope mismatch")
	}
	if doc.Tags == nil {
		return nil, ErrAmbiguousTags
	}
	return decodeTags(doc.Tags, scope)
}

// ReplaceTags sends one document and the complete native ID list, including []
// for explicit clearing. The caller must use the shared merge Writer.
func (s *HTTPStore) ReplaceTags(ctx context.Context, scope Scope, ids []string) error {
	if !s.validScope(scope) || len(ids) > 50 {
		return errors.New("invalid native tag replacement")
	}
	seen := make(map[string]bool)
	for _, id := range ids {
		if !weknora.ValidID(id) || seen[id] {
			return errors.New("invalid or repeated native tag ID")
		}
		seen[id] = true
	}
	body := struct {
		KBID         string   `json:"kb_id"`
		KnowledgeIDs []string `json:"knowledge_ids"`
		TagIDs       []string `json:"tag_ids"`
	}{scope.KnowledgeBaseID, []string{scope.DocumentID}, append(make([]string, 0, len(ids)), ids...)}
	var ack struct {
		Success bool `json:"success"`
	}
	return s.client.JSON(ctx, http.MethodPut, "/api/v1/knowledge/tags", nil, body, &ack)
}

// Catalog resolves the full KB-local tag catalog. It rejects changing totals,
// repeated IDs/names and incomplete pages rather than using a partial mapping.
func (s *HTTPStore) Catalog(ctx context.Context, scope Scope) ([]Tag, error) {
	if !s.validScope(scope) {
		return nil, errors.New("invalid native catalog scope")
	}
	var result []Tag
	ids, names := make(map[string]bool), make(map[string]bool)
	var total int64 = -1
	for page := 1; ; {
		var envelope struct {
			Data *struct {
				weknora.Pagination
				Tags []nativeTag `json:"data"`
			} `json:"data"`
		}
		query := url.Values{"page": {strconv.Itoa(page)}, "page_size": {"50"}}
		if err := s.client.JSON(ctx, http.MethodGet, "/api/v1/knowledge-bases/"+url.PathEscape(scope.KnowledgeBaseID)+"/tags", query, nil, &envelope); err != nil {
			return nil, err
		}
		if envelope.Data == nil || envelope.Data.Tags == nil {
			return nil, errors.New("native catalog data missing")
		}
		next, err := envelope.Data.Pagination.Next(page, 50, len(envelope.Data.Tags))
		if err != nil {
			return nil, err
		}
		if total != -1 && total != *envelope.Data.Total {
			return nil, errors.New("native catalog changed during pagination")
		}
		total = *envelope.Data.Total
		tags, err := decodeTags(envelope.Data.Tags, scope)
		if err != nil {
			return nil, err
		}
		for _, tag := range tags {
			if ids[tag.ID] || names[tag.Name] {
				return nil, errors.New("ambiguous native tag catalog")
			}
			ids[tag.ID], names[tag.Name] = true, true
			result = append(result, tag)
		}
		if next == 0 {
			return result, nil
		}
		page = next
	}
}
