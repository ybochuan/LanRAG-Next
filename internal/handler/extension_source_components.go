package handler

import (
	"strings"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
)

// sourceComponentsDTO is an opt-in proof view. The embedded v1 snapshot/root
// remains complete and unchanged; no source text is returned.
type sourceComponentsDTO struct {
	View        string                       `json:"view"`
	Snapshot    sourceSnapshotDTO            `json:"snapshot"`
	ChunkFields map[string]map[string]string `json:"chunk_fields"`
}

func sourceComponentSelection(c *gin.Context) ([]string, error) {
	query := c.Request.URL.Query()
	view, hasView := query["view"]
	chunks, hasChunks := query["chunk_ids"]
	if !hasView && !hasChunks {
		return nil, nil
	}
	if len(view) != 1 || view[0] != "components-v1" || len(chunks) != 1 {
		return nil, errSourceSnapshotIncomplete
	}
	ids := strings.Split(chunks[0], ",")
	if len(ids) == 0 || len(ids) > 64 {
		return nil, errSourceSnapshotIncomplete
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if len(id) == 0 || len(id) > 128 || strings.TrimSpace(id) != id || strings.ContainsAny(id, "/\\?#%\x00\r\n\t ") || id == "." || id == ".." || seen[id] {
			return nil, errSourceSnapshotIncomplete
		}
		seen[id] = true
	}
	return ids, nil
}

func fingerprintSourceComponents(snapshot sourceSnapshotDTO, chunks []types.Chunk, ids []string) (sourceComponentsDTO, error) {
	result := sourceComponentsDTO{View: "components-v1", Snapshot: snapshot, ChunkFields: map[string]map[string]string{}}
	if len(ids) == 0 || len(ids) > 64 {
		return sourceComponentsDTO{}, errSourceSnapshotIncomplete
	}
	wanted := map[string]bool{}
	for _, id := range ids {
		if id == "" || wanted[id] {
			return sourceComponentsDTO{}, errSourceSnapshotIncomplete
		}
		wanted[id] = true
	}
	leaves := map[string]sourceChunkLeaf{}
	for _, leaf := range snapshot.Chunks {
		leaves[leaf.ID] = leaf
	}
	size := 0
	for _, chunk := range chunks {
		if !wanted[chunk.ID] {
			continue
		}
		leaf, found := leaves[chunk.ID]
		if !found || chunk.TenantID != snapshot.TenantID || chunk.KnowledgeBaseID != snapshot.KnowledgeBaseID || chunk.KnowledgeID != snapshot.DocumentID {
			return sourceComponentsDTO{}, errSourceSnapshotIncomplete
		}
		fields, folded, e := hashSourceFields(nativeChunkFields(chunk), &size)
		if e != nil || folded != leaf.FieldsHash || sourceSHA([]byte(chunk.Content)) != leaf.ContentHash {
			return sourceComponentsDTO{}, errSourceSnapshotIncomplete
		}
		result.ChunkFields[chunk.ID] = fields
	}
	if len(result.ChunkFields) != len(wanted) {
		return sourceComponentsDTO{}, errSourceSnapshotIncomplete
	}
	return result, nil
}
