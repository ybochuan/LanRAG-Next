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
		if r != nil && !isGovernanceWebResult(r) && (cm.KnowledgeIDs == nil || authorized[r.KnowledgeID]) {
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

// sourceInputsForModel describes the fields actually rendered by this pipeline.
// Unsupported composition remains unattributed; citations never fill the gaps.
func sourceInputsForModel(cm *types.ChatManage) ([]extensionbridge.SourceInput, bool) {
	if len(cm.History) != 0 || cm.MemoryPrompt != "" || len(cm.Images) != 0 || len(cm.ContextImages) != 0 || cm.ImageDescription != "" || len(cm.Attachments) != 0 || cm.QuotedContext != "" || cm.DataAnalysisEnabled || cm.EnableRewrite || cm.RerankModelID != "" {
		return nil, false
	}
	if !cm.NeedsRetrieval() {
		return nil, true
	}
	var inputs []extensionbridge.SourceInput
	seen := map[string]bool{}
	for _, r := range cm.MergeResult {
		if r == nil || isGovernanceWebResult(r) || r.ContentRewritten || r.ImageInfo != "" || r.ChunkType == string(types.ChunkTypeFAQ) || r.KnowledgeID == "" || r.ID == "" || r.KnowledgeCustomMetadata != "" {
			return nil, false
		}
		inputs = append(inputs, extensionbridge.SourceInput{DocumentID: r.KnowledgeID, ChunkID: r.ID, Field: "content", ValueHash: extensionbridge.SourceFieldHash(r.Content)})
		if seen[r.KnowledgeID] {
			continue
		}
		seen[r.KnowledgeID] = true
		title, field := r.KnowledgeTitle, "title"
		if title == "" {
			title, field = r.KnowledgeFilename, "file_name"
		}
		if title != "" {
			inputs = append(inputs, extensionbridge.SourceInput{DocumentID: r.KnowledgeID, Field: field, ValueHash: extensionbridge.SourceFieldHash(title)})
			if r.KnowledgeDescription != "" {
				inputs = append(inputs, extensionbridge.SourceInput{DocumentID: r.KnowledgeID, Field: "description", ValueHash: extensionbridge.SourceFieldHash(r.KnowledgeDescription)})
			}
		}
	}
	return inputs, true
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
