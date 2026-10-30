package ingest

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/weknora"
)

// HTTPSource enumerates the public knowledge list without core model imports.
// Offset pages are observations, not a transactional snapshot or deletion signal.
type HTTPSource struct {
	client   *weknora.Client
	pageSize int
}

// NewHTTPSource binds one scoped client and explicit page size.
func NewHTTPSource(client *weknora.Client, pageSize int) (*HTTPSource, error) {
	if client == nil || pageSize < 1 || pageSize > 1000 {
		return nil, errors.New("source client and page size 1..1000 required")
	}
	return &HTTPSource{client, pageSize}, nil
}

// List validates native page metadata and each returned document's effective scope.
func (s *HTTPSource) List(ctx context.Context, scope Scope, page int) (Page, error) {
	var result Page
	if scope.TenantID != s.client.TenantID() || !weknora.ValidID(scope.KnowledgeBaseID) || page < 1 || page > 10000 {
		return result, errors.New("invalid source scope or page")
	}
	var envelope struct {
		weknora.Pagination
		Data json.RawMessage `json:"data"`
	}
	query := url.Values{"page": {strconv.Itoa(page)}, "page_size": {strconv.Itoa(s.pageSize)}, "sort_by": {"created_at"}, "sort_order": {"asc"}}
	if err := s.client.JSON(ctx, http.MethodGet, "/api/v1/knowledge-bases/"+url.PathEscape(scope.KnowledgeBaseID)+"/knowledge", query, nil, &envelope); err != nil {
		return result, err
	}
	if len(envelope.Data) == 0 {
		return result, errors.New("source data field missing")
	}
	var documents []struct {
		ID        string    `json:"id"`
		TenantID  uint64    `json:"tenant_id"`
		KBID      string    `json:"knowledge_base_id"`
		Status    string    `json:"parse_status"`
		UpdatedAt time.Time `json:"updated_at"`
	}
	if err := json.Unmarshal(envelope.Data, &documents); err != nil {
		return result, errors.New("invalid source documents")
	}
	next, err := envelope.Pagination.Next(page, s.pageSize, len(documents))
	if err != nil {
		return result, err
	}
	seen := make(map[string]bool)
	for _, doc := range documents {
		if doc.TenantID != scope.TenantID || doc.KBID != scope.KnowledgeBaseID || !weknora.ValidID(doc.ID) || doc.Status == "" || doc.UpdatedAt.IsZero() || seen[doc.ID] {
			return Page{}, errors.New("invalid source document scope or identity")
		}
		seen[doc.ID] = true
		result.Documents = append(result.Documents, Snapshot{scope, doc.ID, doc.Status, doc.UpdatedAt.UTC()})
	}
	result.NextPage = next
	return result, nil
}
