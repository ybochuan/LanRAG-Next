package agentrag

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"time"
)

const (
	hostMCPName = "AgentRAG"
	hostMCPURL  = "http://127.0.0.1:8090/mcp"
)

var errHostMCPDisabled = errors.New("built-in MCP is disabled")

type hostMCPService struct {
	ID        string `json:"id"`
	TenantID  uint64 `json:"tenant_id"`
	Name      string `json:"name"`
	URL       string `json:"url"`
	Transport string `json:"transport_type"`
	Enabled   bool   `json:"enabled"`
}

func (c *coreClient) provisionHostMCP(ctx context.Context, key string) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1:8090/readyz", nil)
		if err != nil {
			return err
		}
		resp, err := c.http.Do(req)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return c.syncHostMCP(ctx, key)
			}
		}
		select {
		case <-ctx.Done():
			return errors.New("local Host readiness check expired")
		case <-tick.C:
		}
	}
}

func (c *coreClient) syncHostMCP(ctx context.Context, key string) error {
	const path = "/mcp-services"
	var listed struct {
		Success bool             `json:"success"`
		Data    []hostMCPService `json:"data"`
	}
	if err := c.request(ctx, http.MethodGet, path, nil, nil, &listed); err != nil {
		return err
	}
	if !listed.Success {
		return errors.New("Core did not confirm MCP service listing")
	}
	var service hostMCPService
	for _, entry := range listed.Data {
		if entry.Name != hostMCPName || entry.TenantID != c.tenant {
			continue
		}
		if entry.ID == "" || entry.URL != hostMCPURL || entry.Transport != "http-streamable" {
			return errors.New("the local workspace already has another service named AgentRAG")
		}
		service = entry
		break
	}
	if service.ID == "" {
		var created struct {
			Success bool           `json:"success"`
			Data    hostMCPService `json:"data"`
		}
		input := map[string]any{
			"name": hostMCPName, "url": hostMCPURL, "transport_type": "http-streamable",
			"description": "LanRAG Lite 内置的关联文档和历史归档查阅",
			"auth_config": map[string]string{"auth_type": "bearer", "token": key},
		}
		if err := c.request(ctx, http.MethodPost, path, input, nil, &created); err != nil {
			return err
		}
		if !created.Success || created.Data.ID == "" {
			return errors.New("Core did not confirm the built-in MCP service")
		}
		service = created.Data
	}
	if !service.Enabled {
		return errHostMCPDisabled
	}

	var catalog struct {
		Success bool `json:"success"`
		Synced  bool `json:"synced"`
		Data    struct {
			Stale bool `json:"stale"`
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"data"`
	}
	if err := c.request(ctx, http.MethodPost, path+"/"+url.PathEscape(service.ID)+"/metadata/refresh", nil, nil, &catalog); err != nil {
		return err
	}
	tools := make(map[string]bool, len(catalog.Data.Tools))
	for _, tool := range catalog.Data.Tools {
		tools[tool.Name] = true
	}
	if !catalog.Success || !catalog.Synced || catalog.Data.Stale ||
		!tools["query_related_docs"] || !tools["search_archived_documents"] {
		return errors.New("Core MCP catalog is missing the built-in tools")
	}
	return nil
}
