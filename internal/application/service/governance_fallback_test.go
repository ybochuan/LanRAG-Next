package service

import (
	"context"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

type governanceListingKnowledge struct{ interfaces.KnowledgeService }

func (*governanceListingKnowledge) ListKnowledgeByKnowledgeBaseID(context.Context, string) ([]*types.Knowledge, error) {
	return []*types.Knowledge{{ID: "governance-doc", TenantID: 7, KnowledgeBaseID: "governance-kb", EnableStatus: "enabled", Title: "Document", Description: "DESCRIPTION_ONLY_GOVERNANCE_CANARY"}}, nil
}

func TestGovernanceFallbackDescriptionRequiresHostPermission(t *testing.T) {
	service := &sessionService{knowledgeService: &governanceListingKnowledge{}}
	cm := &types.ChatManage{PipelineRequest: types.PipelineRequest{TenantID: 7, KnowledgeBaseIDs: []string{"governance-kb"}, KnowledgeIDs: []string{"governance-doc"}}}
	cm.FallbackPrompt = "{{kb_documents}}"
	prompt, err := service.renderFallbackPrompt(context.WithValue(context.Background(), governanceFixtureKey{}, true), cm)
	if err == nil || strings.Contains(prompt, "DESCRIPTION_ONLY_GOVERNANCE_CANARY") {
		t.Fatal("fallback model received ungoverned document description")
	}
}
