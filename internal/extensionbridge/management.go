package extensionbridge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"time"
)

// ManagementRequest carries only server-resolved authority and independent JSON DTOs.
type ManagementRequest struct {
	Action           string          `json:"action"`
	TenantID         uint64          `json:"tenant_id"`
	KnowledgeBaseID  string          `json:"knowledge_base_id"`
	DocumentID       string          `json:"document_id,omitempty"`
	CanWrite         bool            `json:"can_write"`
	Page             int             `json:"page,omitempty"`
	PageSize         int             `json:"page_size,omitempty"`
	State            string          `json:"state,omitempty"`
	Title            string          `json:"title,omitempty"`
	GraphMode        string          `json:"graph_mode,omitempty"`
	ExpectedRevision int64           `json:"expected_revision"`
	Annotations      json.RawMessage `json:"annotations,omitempty"`
	Rules            json.RawMessage `json:"rules,omitempty"`
	ActorID          string          `json:"actor_id,omitempty"`
	OperationID      string          `json:"operation_id,omitempty"`
	Parents          []string        `json:"parents,omitempty"`
	Business         json.RawMessage `json:"business,omitempty"`
}

// ManagementError exposes a safe status without upstream bodies or credentials.
type ManagementError struct{ Status int }

func (e *ManagementError) Error() string { return "lifecycle management unavailable" }

var managementHTTPClient = &http.Client{Timeout: 2 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

// ManageLifecycle forwards to the fixed loopback host and verifies response scope.
func ManageLifecycle(ctx context.Context, payload ManagementRequest) (json.RawMessage, error) {
	unavailable := &ManagementError{Status: 503}
	key := os.Getenv("AGENTRAG_MANAGEMENT_KEY")
	if len(key) < 32 {
		return nil, unavailable
	}
	body, err := json.Marshal(payload)
	if err != nil || len(body) > 64<<10 {
		return nil, unavailable
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://127.0.0.1:8090/management/lifecycle", bytes.NewReader(body))
	if err != nil {
		return nil, unavailable
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("X-AgentRAG-Bridge-Version", "1")
	response, err := managementHTTPClient.Do(req)
	if err != nil {
		return nil, unavailable
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		status := response.StatusCode
		if status != 400 && status != 403 && status != 404 && status != 409 {
			status = 503
		}
		return nil, &ManagementError{Status: status}
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, (2<<20)+1))
	if err != nil || len(data) > 2<<20 {
		return nil, unavailable
	}
	var envelope struct {
		Success         bool            `json:"success"`
		TenantID        uint64          `json:"tenant_id"`
		KnowledgeBaseID string          `json:"knowledge_base_id"`
		Data            json.RawMessage `json:"data"`
	}
	if json.Unmarshal(data, &envelope) != nil || !envelope.Success || envelope.TenantID != payload.TenantID || envelope.KnowledgeBaseID != payload.KnowledgeBaseID || len(envelope.Data) == 0 || bytes.Equal(envelope.Data, []byte("null")) {
		return nil, unavailable
	}
	return envelope.Data, nil
}

// ManagementStatus normalizes transport and protocol failures to unavailable.
func ManagementStatus(err error) int {
	var e *ManagementError
	if errors.As(err, &e) {
		return e.Status
	}
	return 503
}
