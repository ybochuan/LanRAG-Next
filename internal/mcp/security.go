package mcp

import (
	"crypto/subtle"
	"fmt"
	"os"
	"strings"

	"github.com/Tencent/WeKnora/internal/types"
	secutils "github.com/Tencent/WeKnora/internal/utils"
)

// IsDesktopHostMCP recognizes the fixed authenticated endpoint shipped by Lite Desktop.
func IsDesktopHostMCP(service *types.MCPService) bool {
	if os.Getenv("AGENTRAG_BUILTIN_MCP") != "1" || service == nil ||
		service.URL == nil || *service.URL != "http://127.0.0.1:8090/mcp" ||
		service.TransportType != types.MCPTransportHTTPStreamable || service.Name != "AgentRAG" {
		return false
	}
	auth := service.AuthConfig
	if auth == nil || auth.AuthType != types.MCPAuthBearer || len(service.Headers) != 0 ||
		len(auth.CustomHeaders) != 0 || auth.AuthServerMetadataURL != "" {
		return false
	}
	key := os.Getenv("AGENTRAG_MANAGEMENT_KEY")
	return len(key) >= 32 && subtle.ConstantTimeCompare([]byte(auth.Token), []byte(key)) == 1
}

// ValidateServiceOutboundURLs validates every URL that the MCP transport or
// OAuth discovery flow may contact. It is intentionally called both at
// persistence boundaries and immediately before client construction so stale
// or imported rows cannot bypass the current SSRF policy.
func ValidateServiceOutboundURLs(service *types.MCPService) error {
	if service == nil {
		return fmt.Errorf("MCP service is required")
	}
	if service.URL != nil && !IsDesktopHostMCP(service) {
		serviceURL := strings.TrimSpace(*service.URL)
		if serviceURL != "" {
			if err := secutils.ValidateURLForSSRF(serviceURL); err != nil {
				return fmt.Errorf("MCP service URL failed SSRF validation: %w", err)
			}
		}
	}
	if service.AuthConfig != nil {
		metadataURL := strings.TrimSpace(service.AuthConfig.AuthServerMetadataURL)
		if metadataURL != "" {
			if err := secutils.ValidateURLForSSRF(metadataURL); err != nil {
				return fmt.Errorf("MCP OAuth metadata URL failed SSRF validation: %w", err)
			}
		}
	}
	return nil
}
