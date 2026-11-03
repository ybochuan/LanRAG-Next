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
	tool.mcpTool.Name = "unknown"
	if trustedRelatedTool(tool) {
		t.Fatal("unknown tool trusted")
	}
	tool.mcpTool.Name = "query_related_docs"
	foreign := "http://127.0.0.1:9999/mcp"
	service.URL = &foreign
	if trustedRelatedTool(tool) {
		t.Fatal("same name on another service trusted")
	}
	service.URL = &url
	scoped := RelatedMCPServiceScope(service, types.SearchTargets{&types.SearchTarget{Type: types.SearchTargetTypeKnowledge, KnowledgeBaseID: "kb-a", KnowledgeIDs: []string{"doc-a"}}})
	raw, _ := json.Marshal(scoped)
	if strings.Contains(string(raw), "_native_scope") || len(scoped.AgentRAGScope) == 0 {
		t.Fatal("runtime scope not isolated from stored service")
	}
}
