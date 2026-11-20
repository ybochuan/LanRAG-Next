package extensionbridge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// ManagementRequest carries only server-resolved authority and independent JSON DTOs.
type ManagementRequest struct {
	Action                 string          `json:"action"`
	TenantID               uint64          `json:"tenant_id"`
	KnowledgeBaseID        string          `json:"knowledge_base_id"`
	DocumentID             string          `json:"document_id,omitempty"`
	CanWrite               bool            `json:"can_write"`
	Page                   int             `json:"page,omitempty"`
	PageSize               int             `json:"page_size,omitempty"`
	State                  string          `json:"state,omitempty"`
	Title                  string          `json:"title,omitempty"`
	GraphMode              string          `json:"graph_mode,omitempty"`
	ExpectedRevision       int64           `json:"expected_revision"`
	Annotations            json.RawMessage `json:"annotations,omitempty"`
	Rules                  json.RawMessage `json:"rules,omitempty"`
	ActorID                string          `json:"actor_id,omitempty"`
	OperationID            string          `json:"operation_id,omitempty"`
	Parents                []string        `json:"parents,omitempty"`
	ParentKnowledgeBaseIDs []string        `json:"parent_knowledge_base_ids,omitempty"`
	Business               json.RawMessage `json:"business,omitempty"`
}

// ManagementError exposes a safe status without upstream bodies or credentials.
type ManagementError struct{ Status int }

func (e *ManagementError) Error() string { return "lifecycle management unavailable" }

var managementHTTPClient = &http.Client{Timeout: 2 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
var attachmentManagementHTTPClient = &http.Client{Timeout: 45 * time.Second, Transport: managementHTTPClient.Transport, CheckRedirect: managementHTTPClient.CheckRedirect}

// ManageLifecycle forwards to the fixed loopback host and verifies response scope.
func ManageLifecycle(ctx context.Context, payload ManagementRequest) (json.RawMessage, error) {
	unavailable := &ManagementError{Status: 503}
	key := os.Getenv("AGENTRAG_MANAGEMENT_KEY")
	if len(key) < 32 {
		return nil, unavailable
	}
	body, err := json.Marshal(payload)
	maxBody := 64 << 10
	if payload.Action == "qa_feedback_submit" {
		// Bounded UTF-8 feedback and snapshot fields can expand during JSON escaping.
		maxBody = 192 << 10
	}
	if err != nil || len(body) > maxBody {
		return nil, unavailable
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://127.0.0.1:8090/management/lifecycle", bytes.NewReader(body))
	if err != nil {
		return nil, unavailable
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("X-AgentRAG-Bridge-Version", "1")
	client := managementHTTPClient
	if strings.HasPrefix(payload.Action, "business_attachment_") {
		client = attachmentManagementHTTPClient
	}
	return managementResponse(client, req, payload)
}

// UploadWorkbenchAttachment streams an authorized request to the local attachment importer.
func UploadWorkbenchAttachment(ctx context.Context, payload ManagementRequest, version, filename string, data io.Reader) (json.RawMessage, error) {
	key := os.Getenv("AGENTRAG_MANAGEMENT_KEY")
	if len(key) < 32 || !payload.CanWrite {
		return nil, &ManagementError{Status: 503}
	}
	if payload.DocumentID == "" || len(version) != 32 || filename == "" || len(filename) > 512 {
		return nil, &ManagementError{Status: 400}
	}
	query := url.Values{"tenant_id": {strconv.FormatUint(payload.TenantID, 10)}, "knowledge_base_id": {payload.KnowledgeBaseID},
		"document_id": {payload.DocumentID}, "parent_version": {version}, "file_name": {filename}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://127.0.0.1:8090/management/attachment-upload?"+query.Encode(), data)
	if err != nil {
		return nil, &ManagementError{Status: 503}
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("X-AgentRAG-Bridge-Version", "1")
	req.Header.Set("Content-Type", "application/octet-stream")
	return managementResponse(attachmentManagementHTTPClient, req, payload)
}

func managementResponse(client *http.Client, req *http.Request, payload ManagementRequest) (json.RawMessage, error) {
	unavailable := &ManagementError{Status: 503}
	response, err := client.Do(req)
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
