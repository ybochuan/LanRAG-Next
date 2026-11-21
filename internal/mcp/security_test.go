package mcp

import (
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestValidateServiceOutboundURLsRejectsOAuthMetadataSSRF(t *testing.T) {
	serviceURL := "https://example.com/mcp"
	err := ValidateServiceOutboundURLs(&types.MCPService{
		URL: &serviceURL,
		AuthConfig: &types.MCPAuthConfig{
			AuthServerMetadataURL: "http://169.254.169.254/latest/meta-data",
		},
	})
	if err == nil {
		t.Fatal("expected OAuth metadata URL to be rejected")
	}
}

func TestNewMCPClientRejectsStoredUnsafeURL(t *testing.T) {
	serviceURL := "http://127.0.0.1:8080/mcp"
	_, err := NewMCPClient(&ClientConfig{Service: &types.MCPService{
		URL:           &serviceURL,
		TransportType: types.MCPTransportHTTPStreamable,
	}})
	if err == nil {
		t.Fatal("expected stored unsafe MCP URL to be rejected at client construction")
	}
}

func TestDesktopHostMCPAllowsOnlyAuthenticatedPackagedEndpoint(t *testing.T) {
	serviceURL := "http://127.0.0.1:8090/mcp"
	key := "desktop-host-fixture-key-of-at-least-32-bytes"
	t.Setenv("AGENTRAG_MANAGEMENT_KEY", key)
	t.Setenv("AGENTRAG_BUILTIN_MCP", "1")
	service := types.MCPService{
		Name: "AgentRAG", URL: &serviceURL, TransportType: types.MCPTransportHTTPStreamable,
		AuthConfig: &types.MCPAuthConfig{AuthType: types.MCPAuthBearer, Token: key},
	}
	if _, err := NewMCPClient(&ClientConfig{Service: &service}); err != nil {
		t.Fatalf("packaged Host client: %v", err)
	}
	for _, change := range []struct {
		name string
		edit func(*types.MCPService)
	}{
		{"wrong key", func(s *types.MCPService) { s.AuthConfig.Token = "wrong" }},
		{"other path", func(s *types.MCPService) { u := serviceURL + "/other"; s.URL = &u }},
		{"other service name", func(s *types.MCPService) { s.Name = "other" }},
		{"header override", func(s *types.MCPService) { s.AuthConfig.CustomHeaders = map[string]string{"Host": "elsewhere"} }},
		{"OAuth", func(s *types.MCPService) { s.AuthConfig.AuthType = types.MCPAuthOAuth }},
	} {
		t.Run(change.name, func(t *testing.T) {
			candidate, auth := service, *service.AuthConfig
			candidate.AuthConfig = &auth
			change.edit(&candidate)
			if _, err := NewMCPClient(&ClientConfig{Service: &candidate}); err == nil {
				t.Fatal("accepted an unrelated loopback MCP definition")
			}
		})
	}
	t.Setenv("AGENTRAG_BUILTIN_MCP", "")
	if _, err := NewMCPClient(&ClientConfig{Service: &service}); err == nil {
		t.Fatal("accepted packaged loopback MCP outside Lite Desktop")
	}
}
