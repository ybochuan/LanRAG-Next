// Package extensionbridge contains the thin local HTTP boundary to AgentRAG.
package extensionbridge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

var bridgeHTTPClient = &http.Client{
	Timeout:   500 * time.Millisecond,
	Transport: &http.Transport{Proxy: nil},
	CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

// BeforeRetrieve validates a bounded HTTP policy decision. The native adapter
// must enforce DenyAll and remove ExcludedDocumentIDs before forwarding results.
func BeforeRetrieve(ctx context.Context, payload any) (Decision, error) {
	var decision Decision
	body, err := json.Marshal(payload)
	if err != nil {
		return decision, fmt.Errorf("encode before_retrieve request: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"http://127.0.0.1:8090/bridge/before-retrieve", bytes.NewReader(body))
	if err != nil {
		return decision, fmt.Errorf("create before_retrieve request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-AgentRAG-Bridge-Version", "1")
	response, err := bridgeHTTPClient.Do(request)
	if err != nil {
		return decision, fmt.Errorf("call before_retrieve: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return decision, fmt.Errorf("before_retrieve returned HTTP %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	if err != nil {
		return decision, fmt.Errorf("read before_retrieve response: %w", err)
	}
	return decodeDecision(data, body)
}
