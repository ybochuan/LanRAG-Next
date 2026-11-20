package extensionbridge

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
)

// CorrectionSource identifies an original chunk to read through native authorization.
type CorrectionSource struct {
	DocumentID  string `json:"document_id"`
	ChunkID     string `json:"chunk_id"`
	ContentHash string `json:"content_hash"`
}

// MatchesContent verifies the current original chunk against the reviewed anchor.
func (s CorrectionSource) MatchesContent(content string) bool {
	return content != "" && s.ContentHash == SourceHash([]byte(content))
}

// CorrectionSources looks up candidates inside an already authorized document set.
// No candidate body or authority comes from this optional lookup.
func CorrectionSources(ctx context.Context, tenantID uint64, kbID string, allowedDocIDs []string, query string) []CorrectionSource {
	key := os.Getenv("AGENTRAG_MANAGEMENT_KEY")
	query = strings.TrimSpace(query)
	if len(key) < 32 || tenantID == 0 || !sourceID(kbID) || len(allowedDocIDs) == 0 || len(allowedDocIDs) > MaxRetrieveDocuments || query == "" || len(query) > 4096 {
		return nil
	}
	payload := struct {
		TenantID    uint64   `json:"tenant_id"`
		KBID        string   `json:"knowledge_base_id"`
		DocumentIDs []string `json:"knowledge_ids"`
		Query       string   `json:"query"`
	}{tenantID, kbID, allowedDocIDs, query}
	body, err := json.Marshal(payload)
	if err != nil || len(body) > maxRetrieveBytes {
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://127.0.0.1:8090/bridge/correction-sources", bytes.NewReader(body))
	if err != nil {
		return nil
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("X-AgentRAG-Bridge-Version", "1")
	req.Header.Set("Content-Type", "application/json")
	response, err := bridgeHTTPClient.Do(req)
	if err != nil {
		return nil
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil
	}
	const responseLimit = 8192
	data, err := io.ReadAll(io.LimitReader(response.Body, responseLimit+1))
	if err != nil || len(data) > responseLimit {
		return nil
	}
	var result struct {
		Sources []CorrectionSource `json:"sources"`
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if dec.Decode(&result) != nil || dec.Decode(new(any)) != io.EOF || len(result.Sources) > 12 {
		return nil
	}
	allowed := make(map[string]bool, len(allowedDocIDs))
	for _, id := range allowedDocIDs {
		allowed[id] = true
	}
	seen := make(map[[2]string]bool, len(result.Sources))
	for _, source := range result.Sources {
		key := [2]string{source.DocumentID, source.ChunkID}
		if !allowed[source.DocumentID] || !sourceID(source.ChunkID) || !sourceDigest(source.ContentHash) || seen[key] {
			return nil
		}
		seen[key] = true
	}
	return result.Sources
}

type correctionQueryKey struct{}
type correctionSourceHandlesKey struct{}

// WithCorrectionSourceHandles resolves an original chunk to its current model label.
func WithCorrectionSourceHandles(ctx context.Context, resolve func(string) string) context.Context {
	return context.WithValue(ctx, correctionSourceHandlesKey{}, resolve)
}

// WithCorrectionQuery records the current question for the before-generate hook.
func WithCorrectionQuery(ctx context.Context, query string) context.Context {
	query = strings.TrimSpace(query)
	if query == "" || len(query) > 4096 {
		return ctx
	}
	return context.WithValue(ctx, correctionQueryKey{}, query)
}
