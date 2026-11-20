package chatpipeline

import (
	"context"

	"github.com/Tencent/WeKnora/internal/extensionbridge"
	"github.com/Tencent/WeKnora/internal/types"
)

var governedDocuments = extensionbridge.AllowedDocumentsInScope

func guardPromptResults(ctx context.Context, cm *types.ChatManage) error {
	kbIDs := append([]string(nil), cm.KnowledgeBaseIDs...)
	if len(kbIDs) == 0 {
		kbIDs = cm.SearchTargets.GetAllKnowledgeBaseIDs()
	}
	ids := []string{}
	authorized := map[string]bool{}
	for _, id := range cm.KnowledgeIDs {
		authorized[id] = true
	}
	for _, r := range cm.MergeResult {
		// len==0 means the request carried no per-document restriction; an empty
		// but non-nil slice must not be read as an empty allowlist, which would
		// drop every retrieved passage. The bridge decision already narrowed the
		// candidate set above.
		if r != nil && !isGovernanceWebResult(r) && (len(cm.KnowledgeIDs) == 0 || authorized[r.KnowledgeID]) {
			ids = append(ids, r.KnowledgeID)
		}
	}
	if len(ids) == 0 {
		kept := []*types.SearchResult{}
		for _, r := range cm.MergeResult {
			if r != nil && isGovernanceWebResult(r) {
				kept = append(kept, r)
			}
		}
		cm.MergeResult = kept
		return nil
	}
	if len(kbIDs) == 0 {
		seen := map[string]bool{}
		for _, r := range cm.MergeResult {
			if r != nil && !seen[r.KnowledgeBaseID] {
				kbIDs = append(kbIDs, r.KnowledgeBaseID)
				seen[r.KnowledgeBaseID] = true
			}
		}
	}
	allowed, err := governedDocuments(ctx, cm.TenantID, kbIDs, ids)
	if err != nil {
		return err
	}
	kept := make([]*types.SearchResult, 0, len(cm.MergeResult))
	for _, r := range cm.MergeResult {
		if r != nil && (isGovernanceWebResult(r) || allowed[r.KnowledgeID]) {
			kept = append(kept, r)
		}
	}
	cm.MergeResult = kept
	return nil
}

func isGovernanceWebResult(r *types.SearchResult) bool {
	return r.MatchType == types.MatchTypeWebSearch || r.ChunkType == string(types.ChunkTypeWebSearch)
}

// sourceInputsForModel fingerprints the native fields in the provider's rows.
// The legacy merged passage is replaced by separately rendered source bodies.
func sourceInputsForModel(cm *types.ChatManage) ([]extensionbridge.SourceInput, bool) {
	if len(cm.History) != 0 || cm.MemoryPrompt != "" || len(cm.Images) != 0 || len(cm.ContextImages) != 0 || cm.ImageDescription != "" || len(cm.Attachments) != 0 || cm.QuotedContext != "" || cm.DataAnalysisEnabled {
		return nil, false
	}
	if !cm.NeedsRetrieval() {
		return nil, len(cm.MergeResult) == 0
	}
	for _, r := range cm.MergeResult {
		if !plainModelSource(r) || r.ContentRewritten && len(r.CitationSources) == 0 {
			return nil, false
		}
	}
	var inputs []extensionbridge.SourceInput
	seen := map[string]bool{}
	for _, r := range pipelineModelRows(cm) {
		if !plainModelSource(r) || r.ContentRewritten || r.Content == "" {
			return nil, false
		}
		inputs = append(inputs,
			extensionbridge.SourceInput{DocumentID: r.KnowledgeID, ChunkID: r.ID, Field: "content", ValueHash: extensionbridge.SourceFieldHash(r.Content)},
			extensionbridge.SourceInput{DocumentID: r.KnowledgeID, ChunkID: r.ID, Field: "content_revision", ValueHash: extensionbridge.SourceFieldHash(r.ContentRevision)},
		)
		if seen[r.KnowledgeID] {
			continue
		}
		seen[r.KnowledgeID] = true
		title, field := firstPipelineTitle(r), "title"
		if r.KnowledgeTitle == "" {
			field = "file_name"
		}
		if title != "" {
			inputs = append(inputs, extensionbridge.SourceInput{DocumentID: r.KnowledgeID, Field: field, ValueHash: extensionbridge.SourceFieldHash(title)})
		}
	}
	return inputs, true
}

func plainModelSource(r *types.SearchResult) bool {
	return r != nil && r.ID != "" && r.KnowledgeID != "" && r.KnowledgeBaseID != "" &&
		!isGovernanceWebResult(r) && !isPipelineWebReference(r) && r.MatchType != types.MatchTypeGraph &&
		(r.ChunkType == "" || r.ChunkType == string(types.ChunkTypeText)) &&
		r.ParentChunkID == "" && r.ImageInfo == "" && r.KnowledgeCustomMetadata == ""
}

func finishSourceModelRun(ctx context.Context, call *extensionbridge.SourceCall, success bool) {
	outcome := "error"
	if success {
		outcome = "success"
	}
	if err := call.Finish(outcome); err != nil {
		pipelineWarn(ctx, "Provenance", "model_step_unacknowledged", nil)
	}
	if _, ok := extensionbridge.SourceRunFromContext(ctx); ok {
		if err := extensionbridge.FinishSourceRun(context.WithoutCancel(ctx), !success); err != nil {
			pipelineWarn(ctx, "Provenance", "run_not_sealed", nil)
			// An empty/unsupported run must not remain open merely because its
			// actual execution ended without enough evidence to seal.
			if success {
				_ = extensionbridge.FinishSourceRun(context.WithoutCancel(ctx), true)
			}
		}
	}
}
