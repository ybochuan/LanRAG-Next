package tools

import (
	"encoding/json"
	"github.com/Tencent/WeKnora/internal/types"
	"os"
)

// TrustedRelatedMCPService binds the exception to the authenticated local Host.
func TrustedRelatedMCPService(s *types.MCPService) bool {
	key := os.Getenv("AGENTRAG_MANAGEMENT_KEY")
	return s != nil && s.Enabled && s.URL != nil && *s.URL == "http://127.0.0.1:8090/mcp" && s.TransportType == types.MCPTransportHTTPStreamable && len(key) >= 32 && s.Headers["Authorization"] == "Bearer "+key
}
func trustedRelatedTool(t *MCPTool) bool {
	return t != nil && t.mcpTool != nil && (t.mcpTool.Name == "query_related_docs" || t.mcpTool.Name == "search_archived_documents") && TrustedRelatedMCPService(t.service)
}

// RelatedMCPServiceScope encodes only native-authorized targets, never model args.
func RelatedMCPServiceScope(s *types.MCPService, targets types.SearchTargets) *types.MCPService {
	if !TrustedRelatedMCPService(s) {
		return s
	}
	copy := *s
	copy.Name = "AgentRAG"
	copy.Description = "有效补充通知、附件和历史归档文档查询"
	copy.UsageInstructions = "通过 discover_mcp_tools 按需发现 query_related_docs；用户明确要求查阅历史失效条款时，发现 search_archived_documents。"
	copy.Headers = types.MCPHeaders{}
	for k, v := range s.Headers {
		copy.Headers[k] = v
	}
	ids := []string{}
	whole := []string{}
	var scopedTargets types.SearchTargets
	for _, t := range targets {
		if t == nil || t.KnowledgeBaseID == "" || (t.TenantID != 0 && t.TenantID != s.TenantID) || len(t.TagIDs) != 0 {
			continue
		}
		scopedTargets = append(scopedTargets, t)
		ids = append(ids, t.KnowledgeIDs...)
		if t.Type == types.SearchTargetTypeKnowledgeBase && len(t.KnowledgeIDs) == 0 && len(t.TagIDs) == 0 && len(t.ScopeTagIDs) == 0 {
			whole = append(whole, t.KnowledgeBaseID)
		}
	}
	raw, _ := json.Marshal(map[string]any{"tenant_id": s.TenantID, "knowledge_base_ids": scopedTargets.GetAllKnowledgeBaseIDs(), "knowledge_ids": ids, "whole_kb_ids": whole})
	copy.AgentRAGScope = raw
	return &copy
}
