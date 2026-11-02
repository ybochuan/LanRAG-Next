package extensionbridge

import (
	"context"
	"errors"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
)

// ErrDocumentUnavailable rejects a document lacking a current Host permit.
var ErrDocumentUnavailable = errors.New("document unavailable under governance policy")

// ErrUnattributed rejects derived content whose complete source set is unknown.
var ErrUnattributed = errors.New("M2 governance: complete source provenance unavailable; read blocked")

// AllowedDocuments adapts a native-authorized document set to the existing
// before_retrieve protocol. It contains no lifecycle rules or local fallback.
func AllowedDocuments(ctx context.Context, tenantID uint64, kbID string, ids []string) (map[string]bool, error) {
	return EvaluateDocuments(ctx, tenantID, []string{kbID}, ids)
}

// AllowedDocumentsInScope preserves the complete native-authorized KB scope
// when result DTOs do not carry an individual KB ID (ordinary RAG results).
func AllowedDocumentsInScope(ctx context.Context, tenantID uint64, kbIDs []string, ids []string) (map[string]bool, error) {
	return EvaluateDocuments(ctx, tenantID, kbIDs, ids)
}

// EvaluateDocuments is the native composition dependency. Production keeps its
// fixed HTTP implementation; algorithm unit tests replace it before TestMain
// starts. No environment variable or request field can replace this dependency.
var EvaluateDocuments = allowedDocumentsHTTP

func allowedDocumentsHTTP(ctx context.Context, tenantID uint64, kbIDs []string, ids []string) (map[string]bool, error) {
	requestID, _ := types.RequestIDFromContext(ctx)
	if requestID == "" {
		requestID = uuid.NewString()
	}
	sessionID, _ := ctx.Value(types.SessionIDContextKey).(string)
	if sessionID == "" {
		// Standalone/MCP document reads have no chat session. The existing DTO
		// still requires a nonempty correlation ID; it grants no authority.
		sessionID = requestID
	}
	unique := make([]string, 0, len(ids))
	seen := map[string]bool{}
	for _, id := range ids {
		if id == "" {
			return nil, ErrDocumentUnavailable
		}
		if !seen[id] {
			seen[id] = true
			unique = append(unique, id)
		}
	}
	decision, err := BeforeRetrieve(ctx, map[string]any{"request_id": requestID, "session_id": sessionID, "mode": "agent", "tenant_id": tenantID, "knowledge_base_ids": kbIDs, "knowledge_ids": unique})
	if err != nil {
		return nil, ErrDocumentUnavailable
	}
	allowed := make(map[string]bool, len(decision.KnowledgeIDs))
	for _, id := range decision.KnowledgeIDs {
		allowed[id] = true
	}
	return allowed, nil
}

// RejectUnattributed is the M2 safety boundary approved for historical and
// multi-source content. No persisted records are changed or deleted.
func RejectUnattributed(ctx context.Context) error { return EvaluateUnattributed(ctx) }

// EvaluateUnattributed is paired with EvaluateDocuments for native composition;
// the production default refuses incomplete provenance without reading data.
var EvaluateUnattributed = func(context.Context) error { return ErrUnattributed }
