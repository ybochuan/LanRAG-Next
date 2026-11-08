package extensionbridge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
)

type attachmentDeleteTarget struct {
	DocumentID       string `json:"document_id"`
	KnowledgeBaseID  string `json:"knowledge_base_id"`
	FileHash         string `json:"file_hash"`
	ParentDocumentID string `json:"parent_document_id,omitempty"`
	ImportID         string `json:"import_id,omitempty"`
	IsAttachment     bool   `json:"is_attachment,omitempty"`
}

// PrepareAttachmentDelete asks the local Host to persist the deletion intent
// for files in the explicitly configured Lite worker scope.
func PrepareAttachmentDelete(ctx context.Context, documents []*types.Knowledge) error {
	rawKBs := strings.TrimSpace(os.Getenv("AGENTRAG_KB_IDS"))
	if rawKBs == "" || !strings.EqualFold(strings.TrimSpace(os.Getenv("DB_DRIVER")), "sqlite") {
		return nil
	}
	unavailable := errors.New("attachment delete intent unavailable")
	tenant, err := strconv.ParseUint(os.Getenv("AGENTRAG_TENANT_ID"), 10, 64)
	if err != nil || tenant == 0 {
		return unavailable
	}
	kbs := make(map[string]bool)
	for _, kb := range strings.Split(rawKBs, ",") {
		kbs[strings.TrimSpace(kb)] = true
	}
	var targets []attachmentDeleteTarget
	for _, d := range documents {
		if d.Type != "file" || d.TenantID != tenant || !kbs[d.KnowledgeBaseID] {
			continue
		}
		meta := d.GetMetadata()
		targets = append(targets, attachmentDeleteTarget{DocumentID: d.ID, KnowledgeBaseID: d.KnowledgeBaseID, FileHash: d.FileHash,
			ParentDocumentID: meta["parent_document_id"], ImportID: meta["attachment_import_id"], IsAttachment: meta["is_attachment"] == "true"})
	}
	if len(targets) == 0 {
		return nil
	}
	key := os.Getenv("AGENTRAG_MANAGEMENT_KEY")
	if len(key) < 32 {
		return unavailable
	}
	requestID := uuid.NewString()
	body, err := json.Marshal(struct {
		RequestID string                   `json:"request_id"`
		TenantID  uint64                   `json:"tenant_id"`
		Targets   []attachmentDeleteTarget `json:"targets"`
	}{requestID, tenant, targets})
	if err != nil || len(body) > 4<<20 {
		return unavailable
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://127.0.0.1:8090/bridge/before-attachment-delete", bytes.NewReader(body))
	if err != nil {
		return unavailable
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("X-AgentRAG-Bridge-Version", "1")
	req.Header.Set("Content-Type", "application/json")
	response, err := managementHTTPClient.Do(req)
	if err != nil {
		return unavailable
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNoContent || response.Header.Get("X-AgentRAG-Bridge-Version") != "1" ||
		response.Header.Get("X-AgentRAG-Request-ID") != requestID {
		return unavailable
	}
	return nil
}
