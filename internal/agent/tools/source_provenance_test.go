package tools

import (
	"testing"

	"github.com/Tencent/WeKnora/internal/extensionbridge"
	"github.com/Tencent/WeKnora/internal/types"
)

func TestDocumentSourceInputsRejectForeignChunksAndUnprovedImages(t *testing.T) {
	k := &types.Knowledge{ID: "doc", KnowledgeBaseID: "kb", Title: "TITLE", Metadata: types.JSON(`{"author":"RAW_METADATA"}`)}
	c := &types.Chunk{ID: "chunk", KnowledgeID: "doc", KnowledgeBaseID: "kb", Content: "BODY", ChunkType: types.ChunkTypeText}
	inputs, known := documentSourceInputs(k, []readChunkRow{{chunk: c}})
	if !known {
		t.Fatal("raw text rejected")
	}
	seen := map[string]string{}
	for _, i := range inputs {
		seen[i.Field] = i.ValueHash
	}
	if seen["content"] != extensionbridge.SourceFieldHash("BODY") || seen["metadata"] != extensionbridge.SourceFieldHash([]byte(k.Metadata)) {
		t.Fatal("actual bytes/JSON encoding not bound")
	}
	c.KnowledgeID = "other"
	if _, known = documentSourceInputs(k, []readChunkRow{{chunk: c}}); known {
		t.Fatal("foreign chunk attributed to owning header")
	}
	c.KnowledgeID = "doc"
	c.ImageInfo = "IMAGE_CAPTION"
	if _, known = documentSourceInputs(k, []readChunkRow{{chunk: c}}); known {
		t.Fatal("unproved image composition can seal")
	}
}

func TestSearchSourceInputsPreserveAllReturnedDocuments(t *testing.T) {
	rows := []*searchResultWithMeta{
		{SearchResult: &types.SearchResult{ID: "c1", KnowledgeID: "d1", Content: "BODY1", KnowledgeTitle: "TITLE1"}},
		{SearchResult: &types.SearchResult{ID: "c2", KnowledgeID: "d2", Content: "BODY2", KnowledgeTitle: "TITLE2"}},
	}
	inputs, known := searchSourceInputs(rows)
	seen := map[string]bool{}
	for _, i := range inputs {
		seen[i.DocumentID] = true
	}
	if !known || !seen["d1"] || !seen["d2"] {
		t.Fatal("tool result silently lost a document")
	}
	rows[1].ContentRewritten = true
	if _, known = searchSourceInputs(rows); known {
		t.Fatal("merged body asserted raw source")
	}
}
