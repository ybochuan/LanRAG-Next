package service

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

type sourceMCPInfoFixture struct {
	interfaces.MCPServiceService
	calls int
}

func (s *sourceMCPInfoFixture) ListMCPServicesByIDs(context.Context, uint64, []string) ([]*types.MCPService, error) {
	s.calls++
	return []*types.MCPService{{ID: "mcp", Name: "UNPROVEN_NAME", Description: "UNPROVEN_DESCRIPTION"}}, nil
}

func TestSourceGovernanceRejectsMCPMetadataBeforeItIsLoaded(t *testing.T) {
	ctx := context.WithValue(context.Background(), governanceFixtureKey{}, true)
	ctx = context.WithValue(ctx, types.TenantIDContextKey, uint64(7))
	source := &sourceMCPInfoFixture{}
	svc := &agentService{mcpServiceService: source}
	infos := svc.resolvePinnedMCPServiceInfos(ctx, &types.AgentConfig{PinnedMCPServiceIDs: []string{"mcp"}})
	if len(infos) != 0 || source.calls != 0 {
		t.Fatal("unknown MCP title/description loaded into model metadata")
	}
}
