package tools

import (
	"encoding/json"
	"github.com/Tencent/WeKnora/internal/types"
	"strings"
	"testing"
)

func TestRelatedMCPTrustRequiresLocalServiceAndExactTool(t *testing.T) {
	key := strings.Repeat("x", 32)
	t.Setenv("AGENTRAG_MANAGEMENT_KEY", key)
	url := "http://127.0.0.1:8090/mcp"
	service := &types.MCPService{Enabled: true, TenantID: 7, URL: &url, TransportType: types.MCPTransportHTTPStreamable, Headers: types.MCPHeaders{"Authorization": "Bearer " + key}}
	tool := NewMCPTool(service, &types.MCPTool{Name: "query_related_docs"}, nil, nil, 0)
	if !trustedRelatedTool(tool) {
		t.Fatal("trusted local tool refused")
	}
	tool.mcpTool.Name = "search_archived_documents"
	if !trustedRelatedTool(tool) {
		t.Fatal("trusted local archive tool refused")
	}
	tool.mcpTool.Name = "unknown"
	if trustedRelatedTool(tool) {
		t.Fatal("unknown tool trusted")
	}
	tool.mcpTool.Name = "search_archived_documents"
	foreign := "http://127.0.0.1:9999/mcp"
	service.URL = &foreign
	if trustedRelatedTool(tool) {
		t.Fatal("same name on another service trusted")
	}
	service.URL = &url
	scoped := RelatedMCPServiceScope(service, types.SearchTargets{
		{Type: types.SearchTargetTypeKnowledge, KnowledgeBaseID: "kb-a", KnowledgeIDs: []string{"doc-a"}},
		{Type: types.SearchTargetTypeKnowledge, KnowledgeBaseID: "kb-a", KnowledgeIDs: []string{"tag-constrained-doc"}, TagIDs: []string{"tag"}},
		{Type: types.SearchTargetTypeKnowledge, KnowledgeBaseID: "kb-a", TenantID: 8, KnowledgeIDs: []string{"foreign-doc"}},
	})
	raw, _ := json.Marshal(scoped)
	if strings.Contains(string(raw), "_native_scope") || len(scoped.AgentRAGScope) == 0 {
		t.Fatal("runtime scope not isolated from stored service")
	}
	if scope := string(scoped.AgentRAGScope); !strings.Contains(scope, `"doc-a"`) || strings.Contains(scope, "tag-constrained-doc") || strings.Contains(scope, "foreign-doc") {
		t.Fatal("scoped MCP authority was widened")
	}
}
