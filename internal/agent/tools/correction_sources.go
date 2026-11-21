package tools

import (
	"context"
	"strings"

	"github.com/Tencent/WeKnora/internal/extensionbridge"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

type correctionSourceSearchKey struct{}

// WithCorrectionSourceSearch enables reviewed reverse lookup for a current evidence search.
func WithCorrectionSourceSearch(ctx context.Context) context.Context {
	return context.WithValue(ctx, correctionSourceSearchKey{}, true)
}

func (t *SearchKnowledgeTool) correctionCandidates(
	ctx context.Context, targets types.SearchTargets, kbs []*types.KnowledgeBase, query string,
) []*searchResultWithMeta {
	run, tracked := extensionbridge.SourceRunFromContext(ctx)
	if !tracked || ctx.Value(correctionSourceSearchKey{}) != true || t.chunkService == nil || t.knowledgeService == nil {
		return nil
	}
	var result []*searchResultWithMeta
	for _, kb := range kbs {
		if kb == nil || kb.ID != run.Scope.KnowledgeBaseID || kb.Type != types.KnowledgeBaseTypeDocument || !targets.ContainsKB(kb.ID) {
			continue
		}
		tenant := targets.GetTenantIDForKB(kb.ID)
		if tenant == 0 || tenant != run.Scope.TenantID || tenant != kb.TenantID {
			continue
		}
		kbCtx := context.WithValue(ctx, types.TenantIDContextKey, tenant)
		ids := t.correctionDocuments(kbCtx, targets, kb.ID, tenant)
		if len(ids) == 0 || len(ids) > extensionbridge.MaxRetrieveDocuments {
			continue
		}
		allowed, err := governedDocuments(kbCtx, tenant, kb.ID, ids)
		if err != nil {
			continue
		}
		active := make([]string, 0, len(allowed))
		for _, id := range ids {
			if allowed[id] {
				active = append(active, id)
			}
		}
		for _, source := range extensionbridge.CorrectionSources(kbCtx, tenant, kb.ID, active, query, extensionbridge.OriginalCorrectionQuestion(ctx)) {
			if !allowed[source.DocumentID] {
				continue
			}
			chunk, err := authorizeChunkInSearchTargets(kbCtx, targets, source.ChunkID, t.chunkService, t.knowledgeService)
			if err != nil || !isCurrentCorrectionChunk(chunk, source, kb.ID, tenant) {
				continue
			}
			doc, err := authorizeKnowledgeInSearchTargets(kbCtx, targets, source.DocumentID, t.knowledgeService)
			if err != nil || doc.ID != source.DocumentID || doc.TenantID != tenant || doc.KnowledgeBaseID != kb.ID ||
				doc.EnableStatus != "enabled" || doc.ParseStatus != types.ParseStatusCompleted {
				continue
			}
			result = append(result, &searchResultWithMeta{
				SearchResult: &types.SearchResult{
					ID: chunk.ID, KnowledgeID: doc.ID, KnowledgeBaseID: kb.ID, KnowledgeTitle: doc.Title,
					Content: chunk.Content, ChunkIndex: chunk.ChunkIndex, ChunkType: string(chunk.ChunkType),
					ContentRevision: chunk.ContentRevision, ContextHeader: chunk.ContextHeader,
					StartAt: chunk.StartAt, EndAt: chunk.EndAt, ParentChunkID: chunk.ParentChunkID,
					SourceLocators: chunk.SourceLocators, KnowledgeFilename: doc.FileName, KnowledgeSource: doc.Source,
				},
				SourceQuery: query, QueryType: "correction", KnowledgeBaseID: kb.ID, KnowledgeBaseType: kb.Type,
			})
			if len(result) >= searchKnowledgeMaxLimit {
				return result
			}
		}
	}
	return result
}

// withCorrectionCandidates reserves room for exact reverse hits within the
// existing search limit. Remaining results keep their native rank order.
func withCorrectionCandidates(ranked, candidates []*searchResultWithMeta, limit int) []*searchResultWithMeta {
	if len(candidates) == 0 {
		return ranked
	}
	results := make([]*searchResultWithMeta, 0, limit)
	seen := make(map[string]bool, limit)
	appendUnique := func(rows []*searchResultWithMeta, until int) {
		for _, row := range rows {
			if len(results) == until {
				return
			}
			if !seen[row.ID] {
				seen[row.ID] = true
				results = append(results, row)
			}
		}
	}
	appendUnique(candidates, min(limit, 6))
	appendUnique(ranked, limit)
	return results
}

func isCurrentCorrectionChunk(chunk *types.Chunk, source extensionbridge.CorrectionSource, kbID string, tenant uint64) bool {
	return chunk != nil && chunk.ID == source.ChunkID && chunk.KnowledgeID == source.DocumentID &&
		chunk.KnowledgeBaseID == kbID && chunk.TenantID == tenant && chunk.IsEnabled &&
		(chunk.IndexStatus == "" || chunk.IndexStatus == "ready") && chunk.ChunkType == types.ChunkTypeText && chunk.ImageInfo == "" &&
		strings.TrimSpace(chunk.Content) != "" && source.MatchesContent(chunk.Content)
}

func (t *SearchKnowledgeTool) correctionDocuments(ctx context.Context, targets types.SearchTargets, kbID string, tenant uint64) []string {
	var explicit []string
	listDocuments := false
	for _, target := range targets {
		if target == nil || target.KnowledgeBaseID != kbID || target.TenantID != tenant {
			continue
		}
		ids, tags := searchTargetScope(target)
		explicit = append(explicit, ids...)
		listDocuments = listDocuments || searchTargetIsWholeKB(target) || len(tags) > 0
	}
	if !listDocuments {
		return dedupNonEmptyStrings(explicit)
	}
	docs, err := t.knowledgeService.ListKnowledgeByKnowledgeBaseID(ctx, kbID)
	if err != nil {
		return dedupNonEmptyStrings(explicit)
	}
	rows := make([]*types.SearchResult, 0, len(docs))
	for _, doc := range docs {
		if doc != nil && doc.KnowledgeBaseID == kbID && doc.TenantID == tenant {
			rows = append(rows, &types.SearchResult{KnowledgeID: doc.ID, KnowledgeBaseID: kbID})
		}
	}
	rows, err = filterSearchResultsInSearchTargets(ctx, targets, kbID, rows, t.knowledgeService)
	if err == nil {
		for _, row := range rows {
			explicit = append(explicit, row.KnowledgeID)
		}
	}
	return dedupNonEmptyStrings(explicit)
}

// VerifyKnowledgeFollowUp checks a previous citation against today's native scope,
// exact chunk identity, and active document rule before its user question is reused.
func VerifyKnowledgeFollowUp(ctx context.Context, targets types.SearchTargets, ref *types.SearchResult,
	chunks interfaces.ChunkService, knowledge interfaces.KnowledgeService,
) bool {
	if ref == nil || !targets.ContainsKB(ref.KnowledgeBaseID) || ref.ID == "" || ref.KnowledgeID == "" {
		return false
	}
	tenant := targets.GetTenantIDForKB(ref.KnowledgeBaseID)
	if tenant == 0 {
		return false
	}
	ctx = context.WithValue(ctx, types.TenantIDContextKey, tenant)
	doc, err := authorizeKnowledgeInSearchTargets(ctx, targets, ref.KnowledgeID, knowledge)
	if err != nil || doc.ID != ref.KnowledgeID || doc.KnowledgeBaseID != ref.KnowledgeBaseID || doc.TenantID != tenant ||
		requireGovernedDocument(ctx, doc) != nil {
		return false
	}
	chunk, err := authorizeChunkInSearchTargets(ctx, targets, ref.ID, chunks, knowledge)
	return err == nil && chunk.ID == ref.ID && chunk.KnowledgeID == doc.ID && chunk.KnowledgeBaseID == doc.KnowledgeBaseID && chunk.TenantID == tenant
}
