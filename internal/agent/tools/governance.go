package tools

import (
	"context"
	"errors"

	"github.com/Tencent/WeKnora/internal/extensionbridge"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// Private seams allow existing algorithm unit tests to supply explicit policy
// fixtures. Production always uses the fixed loopback HTTP client.
var governedDocuments = extensionbridge.AllowedDocuments
var rejectUnattributed = extensionbridge.RejectUnattributed

func requireGovernedDocument(ctx context.Context, k *types.Knowledge) error {
	if k == nil {
		return extensionbridge.ErrDocumentUnavailable
	}
	tenant := k.TenantID
	if tenant == 0 {
		tenant, _ = types.TenantIDFromContext(ctx)
	}
	allowed, err := governedDocuments(ctx, tenant, k.KnowledgeBaseID, []string{k.ID})
	if err != nil || !allowed[k.ID] {
		logger.Error(ctx, "[Bridge] document policy failed, blocking knowledge access")
		return extensionbridge.ErrDocumentUnavailable
	}
	return nil
}

func filterGovernedDocuments(ctx context.Context, tenant uint64, kb string, rows []*types.Knowledge) ([]*types.Knowledge, error) {
	ids := make([]string, 0, len(rows))
	for _, k := range rows {
		if k != nil && k.KnowledgeBaseID == kb {
			ids = append(ids, k.ID)
		}
	}
	allowed, err := governedDocuments(ctx, tenant, kb, ids)
	if err != nil {
		return nil, err
	}
	kept := make([]*types.Knowledge, 0, len(rows))
	for _, k := range rows {
		if k != nil && k.KnowledgeBaseID == kb && allowed[k.ID] {
			kept = append(kept, k)
		}
	}
	return kept, nil
}

func filterGovernedSearchResults(ctx context.Context, targets types.SearchTargets, ks interfaces.KnowledgeService, rows []*searchResultWithMeta) ([]*searchResultWithMeta, error) {
	kept := make([]*searchResultWithMeta, 0, len(rows))
	for _, kb := range targets.GetAllKnowledgeBaseIDs() {
		ids := []string{}
		for _, r := range rows {
			if r != nil && r.SearchResult != nil && r.KnowledgeBaseID == kb {
				ids = append(ids, r.KnowledgeID)
			}
		}
		tenant := targets.GetTenantIDForKB(kb)
		if tenant == 0 {
			tenant, _ = types.TenantIDFromContext(ctx)
		}
		allowed, err := governedDocuments(ctx, tenant, kb, ids)
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			if r != nil && r.SearchResult != nil && r.KnowledgeBaseID == kb && allowed[r.KnowledgeID] {
				nativeAllowed, nativeErr := searchTargetsAllowKnowledgeID(ctx, targets, r.KnowledgeID, kb, ks)
				if nativeErr != nil {
					return nil, nativeErr
				}
				if nativeAllowed {
					kept = append(kept, r)
				}
			}
		}
	}
	// Keep the original ranking order. No retrieval/rerank algorithm is changed.
	selected := map[*searchResultWithMeta]bool{}
	for _, r := range kept {
		selected[r] = true
	}
	kept = kept[:0]
	for _, r := range rows {
		if selected[r] {
			kept = append(kept, r)
		}
	}
	return kept, nil
}

func blockedKnowledgeResult(err error) (*types.ToolResult, error) {
	if err == nil {
		err = errors.New("governance read blocked")
	}
	return &types.ToolResult{Success: false, Error: err.Error()}, err
}

// documentSourceInputs fingerprints the raw values consumed by read_document's
// existing formatter. Transformed image/FAQ text needs a separate adapter.
func documentSourceInputs(k *types.Knowledge, rows []readChunkRow) ([]extensionbridge.SourceInput, bool) {
	if k == nil || k.ID == "" {
		return nil, false
	}
	var inputs []extensionbridge.SourceInput
	for field, value := range map[string]any{"title": k.Title, "description": k.Description, "type": k.Type, "source": k.Source, "parse_status": k.ParseStatus, "file_name": k.FileName, "file_type": k.FileType, "file_size": k.FileSize, "metadata": []byte(k.Metadata)} {
		inputs = append(inputs, extensionbridge.SourceInput{DocumentID: k.ID, Field: field, ValueHash: extensionbridge.SourceFieldHash(value)})
	}
	for _, row := range rows {
		c := row.chunk
		if c == nil || c.ID == "" || c.KnowledgeID != k.ID || c.KnowledgeBaseID != k.KnowledgeBaseID || c.ImageInfo != "" || c.ChunkType == types.ChunkTypeFAQ {
			return nil, false
		}
		for field, value := range map[string]any{"content": c.Content, "chunk_type": c.ChunkType, "chunk_index": c.ChunkIndex, "start_at": c.StartAt, "end_at": c.EndAt, "parent_chunk_id": c.ParentChunkID} {
			inputs = append(inputs, extensionbridge.SourceInput{DocumentID: k.ID, ChunkID: c.ID, Field: field, ValueHash: extensionbridge.SourceFieldHash(value)})
		}
	}
	return inputs, true
}

func captureDocumentSource(ctx context.Context, k *types.Knowledge, rows []readChunkRow) error {
	inputs, known := documentSourceInputs(k, rows)
	return extensionbridge.CaptureSourceInputs(ctx, inputs, known)
}

func searchSourceInputs(rows []*searchResultWithMeta) ([]extensionbridge.SourceInput, bool) {
	var inputs []extensionbridge.SourceInput
	for _, row := range rows {
		if row == nil || row.SearchResult == nil || row.KnowledgeID == "" || row.ID == "" || row.ContentRewritten || row.ImageInfo != "" || row.ChunkType == string(types.ChunkTypeFAQ) || row.KnowledgeCustomMetadata != "" {
			return nil, false
		}
		inputs = append(inputs,
			extensionbridge.SourceInput{DocumentID: row.KnowledgeID, ChunkID: row.ID, Field: "content", ValueHash: extensionbridge.SourceFieldHash(row.Content)},
			extensionbridge.SourceInput{DocumentID: row.KnowledgeID, Field: "title", ValueHash: extensionbridge.SourceFieldHash(row.KnowledgeTitle)})
	}
	return inputs, true
}
