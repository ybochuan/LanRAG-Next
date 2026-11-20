package chatpipeline

import (
	"context"
	"errors"

	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/extensionbridge"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// PluginSearchParallel implements parallel search functionality combining chunk search and entity search
type PluginSearchParallel struct {
	// Chunk search dependencies
	knowledgeBaseService interfaces.KnowledgeBaseService
	knowledgeService     interfaces.KnowledgeService
	config               *config.Config
	webSearchService     interfaces.WebSearchService
	tenantService        interfaces.TenantService
	sessionService       interfaces.SessionService

	// Entity search dependencies
	graphRepo     interfaces.RetrieveGraphRepository
	chunkRepo     interfaces.ChunkRepository
	knowledgeRepo interfaces.KnowledgeRepository

	// Internal plugins
	searchPlugin       *PluginSearch
	searchEntityPlugin *PluginSearchEntity
}

// NewPluginSearchParallel creates a new parallel search plugin
func NewPluginSearchParallel(
	eventManager *EventManager,
	knowledgeBaseService interfaces.KnowledgeBaseService,
	knowledgeService interfaces.KnowledgeService,
	chunkService interfaces.ChunkService,
	config *config.Config,
	webSearchService interfaces.WebSearchService,
	tenantService interfaces.TenantService,
	sessionService interfaces.SessionService,
	webSearchStateService interfaces.WebSearchStateService,
	webSearchProviderRepo interfaces.WebSearchProviderRepository,
	graphRepository interfaces.RetrieveGraphRepository,
	chunkRepository interfaces.ChunkRepository,
	knowledgeRepository interfaces.KnowledgeRepository,
) *PluginSearchParallel {
	// Create internal plugins without registering them
	searchPlugin := &PluginSearch{
		knowledgeBaseService:  knowledgeBaseService,
		knowledgeService:      knowledgeService,
		chunkService:          chunkService,
		config:                config,
		webSearchService:      webSearchService,
		tenantService:         tenantService,
		sessionService:        sessionService,
		webSearchStateService: webSearchStateService,
		webSearchProviderRepo: webSearchProviderRepo,
	}

	searchEntityPlugin := &PluginSearchEntity{
		graphRepo:     graphRepository,
		chunkRepo:     chunkRepository,
		knowledgeRepo: knowledgeRepository,
	}

	res := &PluginSearchParallel{
		knowledgeBaseService: knowledgeBaseService,
		knowledgeService:     knowledgeService,
		config:               config,
		webSearchService:     webSearchService,
		tenantService:        tenantService,
		sessionService:       sessionService,
		graphRepo:            graphRepository,
		chunkRepo:            chunkRepository,
		knowledgeRepo:        knowledgeRepository,
		searchPlugin:         searchPlugin,
		searchEntityPlugin:   searchEntityPlugin,
	}
	eventManager.Register(res)
	return res
}

// ActivationEvents returns the event types this plugin handles
func (p *PluginSearchParallel) ActivationEvents() []types.EventType {
	return []types.EventType{types.CHUNK_SEARCH_PARALLEL}
}

// OnEvent handles parallel search events - runs chunk search and entity search concurrently
func (p *PluginSearchParallel) OnEvent(ctx context.Context,
	eventType types.EventType, chatManage *types.ChatManage, next func() *PluginError,
) *PluginError {
	// Intent-based skip: query-understand step determined KB retrieval is unnecessary
	if !chatManage.NeedsRetrieval() {
		pipelineInfo(ctx, "SearchParallel", "skip", map[string]interface{}{
			"session_id": chatManage.SessionID,
			"reason":     "intent_no_search",
		})
		return next()
	}

	requestID, _ := types.RequestIDFromContext(ctx)
	// The Bridge contract requires knowledge_ids to be an already enumerated
	// native-authorized document set; a whole-KB target carries none, and an
	// empty list is answered with deny_all. Expand a plain KB target into its
	// documents here so the policy can subtract non-active ones instead of
	// refusing the whole request.
	candidates := chatManage.KnowledgeIDs
	var scopeErr error
	if len(candidates) == 0 {
		candidates, scopeErr = p.enumerateAuthorizedDocuments(ctx, chatManage)
	}
	if len(candidates) > extensionbridge.MaxRetrieveDocuments {
		scopeErr = extensionbridge.ErrRetrieveScopeTooLarge
	}
	if scopeErr != nil {
		return &PluginError{Err: scopeErr, Description: "Unable to enumerate Bridge retrieval scope", ErrorType: "bridge_before_retrieve_failed"}
	}
	decision, err := extensionbridge.BeforeRetrieve(ctx, map[string]any{
		"request_id":         requestID,
		"session_id":         chatManage.SessionID,
		"mode":               "rag",
		"tenant_id":          chatManage.TenantID,
		"knowledge_base_ids": chatManage.KnowledgeBaseIDs,
		"knowledge_ids":      candidates,
		"original_query":     chatManage.Query,
		"rewritten_query":    chatManage.RewriteQuery,
	})
	if err != nil {
		logger.Error(ctx, "[Bridge] before_retrieve failed, blocking search to prevent policy bypass")
		return &PluginError{
			Err:         err,
			Description: "Extension Bridge before_retrieve failed",
			ErrorType:   "bridge_before_retrieve_failed",
		}
	}
	pipelineInfo(ctx, "SearchParallel", "policy_decision", map[string]interface{}{"candidates": len(candidates), "allowed": len(decision.KnowledgeIDs), "excluded": len(decision.ExcludedDocumentIDs), "deny_all": decision.DenyAll})
	if decision.DenyAll {
		chatManage.SearchResult = nil
		return ErrSearchNothing
	}

	pipelineInfo(ctx, "SearchParallel", "start", map[string]interface{}{
		"session_id":    chatManage.SessionID,
		"has_entities":  len(chatManage.Entity) > 0,
		"rewrite_query": chatManage.RewriteQuery,
	})

	// Deep-copy to avoid concurrent read/write on shared slice fields
	chunkCM := chatManage.Clone()
	chunkCM.SearchResult = nil
	entityCM := chatManage.Clone()
	entityCM.SearchResult = nil

	noop := func() *PluginError { return nil }

	tasks := []ParallelTask{
		{
			Name: "chunk_search",
			Run: func() *PluginError {
				err := p.searchPlugin.OnEvent(ctx, types.CHUNK_SEARCH, chunkCM, noop)
				pipelineInfo(ctx, "SearchParallel", "chunk_search_done", map[string]interface{}{
					"result_count": len(chunkCM.SearchResult),
					"has_error":    err != nil && err != ErrSearchNothing,
				})
				if err == ErrSearchNothing {
					return nil
				}
				return err
			},
		},
		{
			Name: "entity_search",
			Run: func() *PluginError {
				if len(chatManage.Entity) == 0 {
					pipelineInfo(ctx, "SearchParallel", "entity_search_skip", map[string]interface{}{
						"reason": "no_entities",
					})
					return nil
				}
				err := p.searchEntityPlugin.OnEvent(ctx, types.ENTITY_SEARCH, entityCM, noop)
				pipelineInfo(ctx, "SearchParallel", "entity_search_done", map[string]interface{}{
					"result_count": len(entityCM.SearchResult),
					"has_error":    err != nil && err != ErrSearchNothing,
				})
				if err == ErrSearchNothing {
					return nil
				}
				return err
			},
		},
	}

	errs := RunParallel(tasks...)

	// Merge results from both searches
	chatManage.SearchResult = append(chunkCM.SearchResult, entityCM.SearchResult...)
	chatManage.CorrectionCandidates = p.correctionSearchResults(ctx, chatManage, decision.KnowledgeIDs)
	chatManage.SearchResult = removeDuplicateResults(chatManage.SearchResult)
	chatManage.SearchResult = filterBridgeResults(chatManage.SearchResult, decision.KnowledgeIDs, decision.ExcludedDocumentIDs)

	for name, err := range errs {
		logger.Warnf(ctx, "[SearchParallel] %s error: %v", name, err.Err)
	}

	pipelineInfo(ctx, "SearchParallel", "complete", map[string]interface{}{
		"session_id":     chatManage.SessionID,
		"chunk_results":  len(chunkCM.SearchResult),
		"entity_results": len(entityCM.SearchResult),
		"total_results":  len(chatManage.SearchResult),
		"error_count":    len(errs),
	})

	if len(chatManage.SearchResult) == 0 && len(chatManage.CorrectionCandidates) == 0 {
		if err, ok := errs["chunk_search"]; ok {
			return err
		}
		return ErrSearchNothing
	}

	return next()
}

// enumerateAuthorizedDocuments expands plain whole-KB targets into the
// explicit document IDs the Bridge policy takes as its candidate set. Targets
// with an unresolved document or tag scope cannot be expanded to whole KBs.
func (p *PluginSearchParallel) enumerateAuthorizedDocuments(ctx context.Context, cm *types.ChatManage) ([]string, error) {
	if p.knowledgeService == nil || len(cm.SearchTargets) == 0 {
		return nil, errors.New("no authorized retrieval scope")
	}
	seen := map[string]bool{}
	ids := []string{}
	for _, target := range cm.SearchTargets {
		if target == nil || target.Type != types.SearchTargetTypeKnowledgeBase || len(target.KnowledgeIDs) > 0 || len(target.TagIDs) > 0 || len(target.ScopeTagIDs) > 0 {
			return nil, errors.New("no authorized document IDs for the requested retrieval scope")
		}
		docs, err := p.knowledgeService.ListKnowledgeByKnowledgeBaseID(ctx, target.KnowledgeBaseID)
		if err != nil {
			return nil, errors.New("failed to enumerate authorized documents for retrieval")
		}
		for _, doc := range docs {
			if doc == nil || doc.ID == "" || doc.EnableStatus != "enabled" || doc.ParseStatus != types.ParseStatusCompleted || seen[doc.ID] {
				continue
			}
			seen[doc.ID] = true
			ids = append(ids, doc.ID)
			if len(ids) > extensionbridge.MaxRetrieveDocuments {
				return nil, extensionbridge.ErrRetrieveScopeTooLarge
			}
		}
	}
	pipelineInfo(ctx, "SearchParallel", "enumerated_documents", map[string]interface{}{"count": len(ids), "targets": len(cm.SearchTargets)})
	return ids, nil
}

// Apply the same exclusion to chunk and entity results before any later stage.
func filterBridgeResults(results []*types.SearchResult, allowedIDs, excludedIDs []string) []*types.SearchResult {
	allowed := make(map[string]bool, len(allowedIDs))
	for _, id := range allowedIDs {
		allowed[id] = true
	}
	excluded := make(map[string]bool, len(excludedIDs))
	for _, id := range excludedIDs {
		excluded[id] = true
	}
	filtered := results[:0]
	for _, result := range results {
		if result != nil && !excluded[result.KnowledgeID] && (allowed[result.KnowledgeID] || result.MatchType == types.MatchTypeWebSearch) {
			filtered = append(filtered, result)
		}
	}
	return filtered
}
