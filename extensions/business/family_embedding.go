package business

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"
)

const familyEmbeddingBatch = 16

// LoadFamilyEmbedder enables semantic suggestions only when a local origin
// and an explicit model name are supplied.
func LoadFamilyEmbedder(getenv func(string) string) (*LocalFamilyEmbedder, error) {
	origin := strings.TrimSpace(getenv("AGENTRAG_FAMILY_EMBEDDING_ORIGIN"))
	model := strings.TrimSpace(getenv("AGENTRAG_FAMILY_EMBEDDING_MODEL"))
	if origin == "" && model == "" {
		return nil, nil
	}
	return NewLocalFamilyEmbedder(origin, model)
}

// LocalFamilyEmbedder requests title embeddings from an explicitly configured loopback service.
type LocalFamilyEmbedder struct {
	endpoint string
	model    string
	client   *http.Client
}

// NewLocalFamilyEmbedder validates a literal loopback origin. It never forwards
// Core credentials and cannot follow redirects or use environment proxies.
func NewLocalFamilyEmbedder(origin, model string) (*LocalFamilyEmbedder, error) {
	u, err := url.Parse(origin)
	if err != nil || u.Scheme != "http" || u.User != nil || u.ForceQuery || u.RawQuery != "" || u.Fragment != "" ||
		(u.Path != "" && u.Path != "/") || !net.ParseIP(u.Hostname()).IsLoopback() ||
		model == "" || strings.TrimSpace(model) != model || len(model) > 160 || strings.IndexFunc(model, unicode.IsControl) >= 0 {
		return nil, errors.New("invalid local family embedding configuration")
	}
	transport := &http.Transport{DialContext: (&net.Dialer{Timeout: 2 * time.Second}).DialContext, IdleConnTimeout: 30 * time.Second}
	return &LocalFamilyEmbedder{endpoint: strings.TrimSuffix(u.String(), "/") + "/v1/embeddings", model: model,
		client: &http.Client{Transport: transport, Timeout: 8 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

// WithFamilyEmbedder installs optional local semantic scoring before workers start.
func (s *Service) WithFamilyEmbedder(model *LocalFamilyEmbedder) *Service {
	s.familyEmbedder = model
	return s
}

func (e *LocalFamilyEmbedder) embed(ctx context.Context, titles []string) ([][]float64, error) {
	if len(titles) < 1 || len(titles) > familyEmbeddingBatch {
		return nil, ErrInvalid
	}
	input := make([]string, len(titles))
	for i, title := range titles {
		runes := []rune(strings.TrimSpace(title))
		input[i] = string(runes[:min(len(runes), 512)])
	}
	payload, err := json.Marshal(struct {
		Model string   `json:"model"`
		Input []string `json:"input"`
	}{e.model, input})
	if err != nil {
		return nil, ErrUnavailable
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, ErrUnavailable
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := e.client.Do(req)
	if err != nil {
		return nil, ErrUnavailable
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, ErrUnavailable
	}
	const maxBytes = 4 << 20
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil || len(raw) > maxBytes {
		return nil, ErrUnavailable
	}
	var result struct {
		Model string `json:"model"`
		Data  []struct {
			Index     *int      `json:"index"`
			Embedding []float64 `json:"embedding"`
		} `json:"data"`
	}
	if json.Unmarshal(raw, &result) != nil || len(result.Data) != len(titles) ||
		(result.Model != "" && result.Model != e.model) {
		return nil, ErrUnavailable
	}
	vectors := make([][]float64, len(titles))
	dim := len(result.Data[0].Embedding)
	if dim < 2 || dim > 8192 {
		return nil, ErrUnavailable
	}
	for _, row := range result.Data {
		if row.Index == nil || *row.Index < 0 || *row.Index >= len(vectors) || vectors[*row.Index] != nil ||
			len(row.Embedding) != dim || !normalizeFamilyVector(row.Embedding) {
			return nil, ErrUnavailable
		}
		vectors[*row.Index] = row.Embedding
	}
	return vectors, nil
}

func normalizeFamilyVector(vector []float64) bool {
	var norm float64
	for _, value := range vector {
		norm = math.Hypot(norm, value)
	}
	if norm == 0 || math.IsNaN(norm) || math.IsInf(norm, 0) {
		return false
	}
	for i := range vector {
		vector[i] /= norm
	}
	return true
}

func applyFamilySemanticEvidence(e *SuggestionEvidence, a, b []float64, model string) bool {
	if len(a) == 0 || len(a) != len(b) {
		return false
	}
	var dot float64
	for i, value := range a {
		dot += value * b[i]
	}
	value := max(0, min(1, dot))
	e.SemanticSimilarity, e.SemanticModel = &value, model
	e.Similarity = max(e.Similarity, value)
	e.Reasons = append(e.Reasons, "local_title_embedding")
	return value > 0
}
