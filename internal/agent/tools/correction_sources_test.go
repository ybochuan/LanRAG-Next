package tools

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/extensionbridge"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestCorrectionCandidateRequiresCurrentOriginal(t *testing.T) {
	chunk := types.Chunk{
		ID: "chunk", KnowledgeID: "doc", KnowledgeBaseID: "kb", TenantID: 10,
		Content: "ordinary reviewed original", IsEnabled: true, IndexStatus: "ready", ChunkType: types.ChunkTypeText,
	}
	source := extensionbridge.CorrectionSource{
		DocumentID: chunk.KnowledgeID, ChunkID: chunk.ID, ContentHash: extensionbridge.SourceHash([]byte(chunk.Content)),
	}
	require.True(t, isCurrentCorrectionChunk(&chunk, source, "kb", 10))
	for name, change := range map[string]func(*types.Chunk){
		"edited":           func(c *types.Chunk) { c.Content += " edited" },
		"disabled":         func(c *types.Chunk) { c.IsEnabled = false },
		"processing":       func(c *types.Chunk) { c.IndexStatus = "processing" },
		"foreign document": func(c *types.Chunk) { c.KnowledgeID = "other" },
		"foreign kb":       func(c *types.Chunk) { c.KnowledgeBaseID = "other" },
		"foreign tenant":   func(c *types.Chunk) { c.TenantID++ },
		"different chunk":  func(c *types.Chunk) { c.ID = "other" },
	} {
		t.Run(name, func(t *testing.T) {
			changed := chunk
			change(&changed)
			require.False(t, isCurrentCorrectionChunk(&changed, source, "kb", 10))
		})
	}
}

func TestCorrectionCandidatesKeepNativeLimitAndRemainingRank(t *testing.T) {
	row := func(id, content string) *searchResultWithMeta {
		return &searchResultWithMeta{SearchResult: &types.SearchResult{ID: id, Content: content}}
	}
	original := row("anchor", "current body")
	stale := row("anchor", "stale body")
	first, last := row("first", "native first"), row("last", "native last")
	require.Equal(t, []*searchResultWithMeta{original, first},
		withCorrectionCandidates([]*searchResultWithMeta{first, stale, last}, []*searchResultWithMeta{original, original}, 2))
}

func TestKnowledgeFollowUpRequiresCurrentAuthorizedCitation(t *testing.T) {
	const tenant = 10
	for _, tc := range []struct {
		name, document, selectedDocument, chunkDocument string
		enabled, expected                               bool
	}{
		{name: "current citation", document: "doc", selectedDocument: "doc", chunkDocument: "doc", enabled: true, expected: true},
		{name: "outside selected documents", document: "doc", selectedDocument: "other", chunkDocument: "doc", enabled: true},
		{name: "revoked document", document: "governance-revoked", selectedDocument: "governance-revoked", chunkDocument: "governance-revoked", enabled: true},
		{name: "disabled chunk", document: "doc", selectedDocument: "doc", chunkDocument: "doc"},
		{name: "different current chunk owner", document: "doc", selectedDocument: "doc", chunkDocument: "other", enabled: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			targets := types.SearchTargets{{
				Type: types.SearchTargetTypeKnowledge, KnowledgeBaseID: "kb", TenantID: tenant, KnowledgeIDs: []string{tc.selectedDocument},
			}}
			chunks := &scopeChunkService{chunk: &types.Chunk{
				ID: "chunk", KnowledgeID: tc.chunkDocument, KnowledgeBaseID: "kb", TenantID: tenant, IsEnabled: tc.enabled,
			}}
			knowledge := &scopeKnowledgeService{knowledge: &types.Knowledge{ID: tc.document, KnowledgeBaseID: "kb", TenantID: tenant}}
			ref := &types.SearchResult{ID: "chunk", KnowledgeID: tc.document, KnowledgeBaseID: "kb"}
			require.Equal(t, tc.expected, VerifyKnowledgeFollowUp(context.Background(), targets, ref, chunks, knowledge))
		})
	}
}
