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
	Title            string `json:"title,omitempty"`
	Resources        string `json:"resources,omitempty"`
}

// ErrAttachmentDeleteConflict indicates a live reference or conflicting attachment receipt.
var ErrAttachmentDeleteConflict = errors.New("attachment delete conflict")

func ValidateAttachmentDelete(ctx context.Context, documents []*types.Knowledge) error {
	return prepareAttachmentDelete(ctx, documents, attachmentDeleteOptions{CheckOnly: true})
}

// PrepareAttachmentDelete asks the local Host to persist the deletion intent
// for files in the explicitly configured Lite worker scope.
func PrepareAttachmentDelete(ctx context.Context, documents []*types.Knowledge, resources ...map[string]AttachmentResourceInventory) error {
	options := attachmentDeleteOptions{}
	if len(resources) > 0 {
		options.Resources = resources[0]
	}
	return prepareAttachmentDelete(ctx, documents, options)
}

type attachmentDeleteOptions struct {
	CheckOnly      bool
	Reservation    string
	Cancel         bool
	Transfer       bool
	IndexesCleared bool
	Resources      map[string]AttachmentResourceInventory
}

// ConfirmAttachmentIndexes persists the successful native index cleanup checkpoint.
func ConfirmAttachmentIndexes(ctx context.Context, documents []*types.Knowledge) error {
	return prepareAttachmentDelete(ctx, documents, attachmentDeleteOptions{IndexesCleared: true})
}

// ValidateAttachmentTransfer checks that moving files cannot strand managed attachments.
func ValidateAttachmentTransfer(ctx context.Context, documents []*types.Knowledge) error {
	return prepareAttachmentDelete(ctx, documents, attachmentDeleteOptions{CheckOnly: true, Transfer: true})
}

// ReserveAttachmentDelete fences a replacement until the native row transaction resolves.
func ReserveAttachmentDelete(ctx context.Context, documents []*types.Knowledge, resources map[string]AttachmentResourceInventory, reservation string) error {
	return prepareAttachmentDelete(ctx, documents, attachmentDeleteOptions{Reservation: reservation, Resources: resources})
}

// CancelAttachmentDelete releases only this replacement's still-live native targets.
func CancelAttachmentDelete(ctx context.Context, documents []*types.Knowledge, reservation string) error {
	return prepareAttachmentDelete(ctx, documents, attachmentDeleteOptions{Reservation: reservation, Cancel: true})
}

// AttachmentManagedKnowledge identifies records within the configured attachment worker scope.
func AttachmentManagedKnowledge(d *types.Knowledge) bool {
	if d == nil || d.Type != "file" || !strings.EqualFold(strings.TrimSpace(os.Getenv("DB_DRIVER")), "sqlite") {
		return false
	}
	tenant, _ := strconv.ParseUint(os.Getenv("AGENTRAG_TENANT_ID"), 10, 64)
	if tenant == 0 || d.TenantID != tenant {
		return false
	}
	for _, kb := range strings.Split(os.Getenv("AGENTRAG_KB_IDS"), ",") {
		if d.KnowledgeBaseID == strings.TrimSpace(kb) {
			return true
		}
	}
	return false
}

func prepareAttachmentDelete(ctx context.Context, documents []*types.Knowledge, options attachmentDeleteOptions) error {
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
		sealed := ""
		if inventory, ok := options.Resources[d.ID]; ok {
			var err error
			sealed, err = SignAttachmentResources(AttachmentResources{Tenant: tenant, KB: d.KnowledgeBaseID, Document: d.ID, Hash: d.FileHash, References: inventory.References, Unresolved: inventory.Unresolved})
			if err != nil {
				return err
			}
		}
		targets = append(targets, attachmentDeleteTarget{DocumentID: d.ID, KnowledgeBaseID: d.KnowledgeBaseID, FileHash: d.FileHash,
			ParentDocumentID: meta["parent_document_id"], ImportID: meta["attachment_import_id"], IsAttachment: meta["is_attachment"] == "true", Title: d.Title, Resources: sealed})
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
		RequestID      string                   `json:"request_id"`
		TenantID       uint64                   `json:"tenant_id"`
		Targets        []attachmentDeleteTarget `json:"targets"`
		CheckOnly      bool                     `json:"check_only,omitempty"`
		Reservation    string                   `json:"reservation_id,omitempty"`
		Cancel         bool                     `json:"cancel,omitempty"`
		Transfer       bool                     `json:"transfer,omitempty"`
		IndexesCleared bool                     `json:"indexes_cleared,omitempty"`
	}{requestID, tenant, targets, options.CheckOnly, options.Reservation, options.Cancel, options.Transfer, options.IndexesCleared})
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
	if response.StatusCode == http.StatusConflict && response.Header.Get("X-AgentRAG-Bridge-Version") == "1" {
		return ErrAttachmentDeleteConflict
	}
	if response.StatusCode != http.StatusNoContent || response.Header.Get("X-AgentRAG-Bridge-Version") != "1" ||
		response.Header.Get("X-AgentRAG-Request-ID") != requestID {
		return unavailable
	}
	return nil
}
