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
		if i.AgentSearch {
			t.Fatal("direct document read marked as an Agent search")
		}
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
		if i.AgentSearch != (i.Field == "content" && i.ChunkID != "") {
			t.Fatalf("unexpected retrieval marker for %q", i.Field)
		}
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

func TestRerankSourceInputsProveOnlyFieldsThatExplainSentText(t *testing.T) {
	row := &types.SearchResult{
		ID: "chunk", KnowledgeID: "doc", Content: "**Reviewed** by two people",
		KnowledgeTitle: "Policy", ContextHeader: "Section 1",
	}
	rows := []*types.SearchResult{row}
	passage := "Policy\n\nSection 1\n\nReviewed"
	inputs, known := rerankSourceInputs(rows, []string{passage}, false)
	if !known || len(inputs) != 3 {
		t.Fatalf("cleaned, fitted text lacked evidence: %+v", inputs)
	}
	fields := map[string]any{"content": row.Content, "title": row.KnowledgeTitle, "context_header": row.ContextHeader}
	for _, input := range inputs {
		if input.AgentSearch || input.ValueHash != extensionbridge.SourceFieldHash(fields[input.Field]) {
			t.Fatalf("rerank candidate was marked retrieved or did not match a raw field: %+v", input)
		}
		delete(fields, input.Field)
	}
	if len(fields) != 0 {
		t.Fatalf("missing evidence fields: %v", fields)
	}
	if _, known = rerankSourceInputs(rows, []string{passage + " with unproved enrichment"}, false); known {
		t.Fatal("unproved enrichment can seal")
	}
	if _, known = rerankSourceInputs(rows, []string{passage}, true); known {
		t.Fatal("image request can seal with text evidence")
	}
}
