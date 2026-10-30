// Package extensionbridge contains the thin local HTTP boundary to AgentRAG.
package extensionbridge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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

// BeforeRetrieve performs the HTTP gate only; it does not alter native search
// targets or implement governance filtering. Only HTTP 200 permits continuation.
func BeforeRetrieve(ctx context.Context, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode before_retrieve request: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"http://127.0.0.1:8090/bridge/before-retrieve", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create before_retrieve request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-AgentRAG-Bridge-Version", "1")
	response, err := bridgeHTTPClient.Do(request)
	if err != nil {
		return fmt.Errorf("call before_retrieve: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("before_retrieve returned HTTP %d", response.StatusCode)
	}
	return nil
}
