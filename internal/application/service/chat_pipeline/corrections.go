package chatpipeline

import (
	"context"
	"slices"
	"strings"

	"github.com/Tencent/WeKnora/internal/extensionbridge"
	"github.com/Tencent/WeKnora/internal/types"
)

func correctionQuery(cm *types.ChatManage) string {
	query, rewrite := strings.TrimSpace(cm.Query), strings.TrimSpace(cm.RewriteQuery)
	if rewrite != "" && query != rewrite && len(query)+len(rewrite)+1 <= 4096 {
		return query + "\n" + rewrite
	}
	return query
}

// correctionSearchResults reads current original rows for scoped reverse hits.
// The Host suggestion never supplies model text or widens the native search grant.
func (p *PluginSearchParallel) correctionSearchResults(ctx context.Context, cm *types.ChatManage, allowed []string) []*types.SearchResult {
	run, tracked := extensionbridge.SourceRunFromContext(ctx)
	if !tracked || p.chunkRepo == nil || p.knowledgeRepo == nil {
		return nil
	}
	tenantID, kbID := run.Scope.TenantID, run.Scope.KnowledgeBaseID
	candidates := extensionbridge.CorrectionSources(ctx, tenantID, kbID, allowed, correctionQuery(cm), cm.Query)
	var results []*types.SearchResult
	for _, candidate := range candidates {
		chunk, err := p.chunkRepo.GetChunkByID(ctx, tenantID, candidate.ChunkID)
		if err != nil || chunk == nil || chunk.ID != candidate.ChunkID || chunk.KnowledgeID != candidate.DocumentID ||
			chunk.TenantID != tenantID || chunk.KnowledgeBaseID != kbID || !chunk.IsEnabled ||
			(chunk.IndexStatus != "" && chunk.IndexStatus != "ready") || !candidate.MatchesContent(chunk.Content) ||
			!slices.ContainsFunc(cm.SearchTargets, func(target *types.SearchTarget) bool {
				return correctionTargetContains(target, chunk)
			}) {
			continue
		}
		doc, err := p.knowledgeRepo.GetKnowledgeByID(ctx, tenantID, candidate.DocumentID)
		if err != nil || doc == nil || doc.ID != candidate.DocumentID || doc.TenantID != tenantID || doc.KnowledgeBaseID != kbID ||
			doc.EnableStatus != "enabled" || doc.ParseStatus != types.ParseStatusCompleted {
			continue
		}
		result := chunk2SearchResult(chunk, doc)
		result.MatchType = types.MatchTypeKeywords
		if plainModelSource(result) {
			results = append(results, result)
		}
	}
	return results
}

func correctionTargetContains(target *types.SearchTarget, chunk *types.Chunk) bool {
	if target == nil || target.TenantID != chunk.TenantID || target.KnowledgeBaseID != chunk.KnowledgeBaseID ||
		(len(target.TagIDs) > 0 && !slices.Contains(target.TagIDs, chunk.TagID)) {
		return false
	}
	switch target.Type {
	case types.SearchTargetTypeKnowledgeBase:
		return len(target.ScopeTagIDs) == 0 || len(target.TagIDs) > 0
	case types.SearchTargetTypeKnowledge:
		return slices.Contains(target.KnowledgeIDs, chunk.KnowledgeID)
	default:
		return false
	}
}

// withCorrectionCandidates puts exact reverse hits into the existing context budget.
// The remaining slots retain their native ranking order.
func withCorrectionCandidates(ranked, candidates []*types.SearchResult, topK int) []*types.SearchResult {
	if len(candidates) == 0 {
		return ranked
	}
	correctionLimit := len(candidates)
	if topK > 0 {
		correctionLimit = min(correctionLimit, topK)
	}
	correctionLimit = min(correctionLimit, 6)
	results := append(slices.Clone(candidates[:correctionLimit]), ranked...)
	results = removeDuplicateResults(results)
	if topK > 0 && len(results) > topK {
		results = results[:topK]
	}
	return results
}

func attachCorrectionReferences(results []*types.SearchResult, items []types.CorrectionReference) {
	for _, item := range items {
		for _, result := range results {
			if result == nil || result.KnowledgeID != item.DocumentID {
				continue
			}
			if result.ID == item.ChunkID || slices.ContainsFunc(result.CitationSources, func(source *types.SearchResult) bool {
				return source != nil && source.ID == item.ChunkID && source.KnowledgeID == item.DocumentID
			}) {
				result.CorrectionItems = append(result.CorrectionItems, item)
				break
			}
		}
	}
}
