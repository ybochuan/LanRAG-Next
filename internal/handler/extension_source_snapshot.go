package handler

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const sourceSnapshotScheme = "agentrag-native-snapshot-v1"

var errSourceSnapshotIncomplete = errors.New("source snapshot incomplete or exceeds limits")

type sourceChunkLeaf struct {
	ID              string `json:"id"`
	ContentRevision int    `json:"content_revision"`
	ChunkIndex      int    `json:"chunk_index"`
	ContentHash     string `json:"content_hash"`
	FieldsHash      string `json:"fields_hash"`
}

type sourceSnapshotDTO struct {
	Scheme          string            `json:"scheme"`
	TenantID        uint64            `json:"tenant_id"`
	KnowledgeBaseID string            `json:"knowledge_base_id"`
	DocumentID      string            `json:"document_id"`
	HeaderFields    map[string]string `json:"header_fields"`
	HeaderHash      string            `json:"header_hash"`
	Chunks          []sourceChunkLeaf `json:"chunks"`
	RootHash        string            `json:"root_hash"`
}

func sourceSHA(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// hashSourceFields freezes a name/hash list, independently of map iteration.
func hashSourceFields(values map[string]any, size *int) (map[string]string, string, error) {
	fields := make(map[string]string, len(values))
	keys := make([]string, 0, len(values))
	for name, value := range values {
		encoded, err := json.Marshal(value)
		if err != nil {
			return nil, "", errSourceSnapshotIncomplete
		}
		*size += len(encoded)
		if *size > 64<<20 {
			return nil, "", errSourceSnapshotIncomplete
		}
		fields[name] = sourceSHA(encoded)
		keys = append(keys, name)
	}
	sort.Strings(keys)
	pairs := make([][2]string, 0, len(keys))
	for _, name := range keys {
		pairs = append(pairs, [2]string{name, fields[name]})
	}
	encoded, _ := json.Marshal(pairs)
	return fields, sourceSHA(encoded), nil
}

func nativeSourceHeader(d types.Knowledge) map[string]any {
	var profile any
	if d.Profile != nil {
		profile = map[string]any{"gist": d.Profile.Gist, "topics": d.Profile.Topics, "doc_type": d.Profile.DocType, "typical_question": d.Profile.TypicalQuestion}
	}
	return map[string]any{
		"type": d.Type, "title": d.Title, "description": d.Description, "source": d.Source,
		"channel": d.Channel, "parse_status": d.ParseStatus, "summary_status": d.SummaryStatus,
		"profile": profile, "enable_status": d.EnableStatus, "file_name": d.FileName,
		"folder_path": d.FolderPath, "file_type": d.FileType, "file_size": d.FileSize,
		"file_hash": d.FileHash, "metadata": []byte(d.Metadata), "custom_metadata": []byte(d.CustomMetadata),
		"created_at": d.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
}

func nativeChunkFields(c types.Chunk) map[string]any {
	var locators []map[string]any
	if c.SourceLocators != nil {
		locators = make([]map[string]any, 0, len(c.SourceLocators))
		for _, p := range c.SourceLocators {
			locators = append(locators, map[string]any{"type": p.Type, "mapping": p.Mapping, "partial": p.Partial,
				"source_id": p.SourceID, "source_hash": p.SourceHash, "page": p.Page, "bbox": p.BBox,
				"block": p.Block, "slide": p.Slide, "sheet": p.Sheet, "row_start": p.RowStart, "row_end": p.RowEnd,
				"start": p.Start, "end": p.End, "start_ms": p.StartMs, "end_ms": p.EndMs, "section": p.Section, "title": p.Title, "quote": p.Quote})
		}
	}
	return map[string]any{
		"content": c.Content, "source_content": c.SourceContent, "content_revision": c.ContentRevision,
		"index_status": c.IndexStatus, "chunk_index": c.ChunkIndex, "is_enabled": c.IsEnabled,
		"flags": c.Flags, "status": c.Status, "start_at": c.StartAt, "end_at": c.EndAt,
		"pre_chunk_id": c.PreChunkID, "next_chunk_id": c.NextChunkID, "chunk_type": c.ChunkType,
		"parent_chunk_id": c.ParentChunkID, "relation_chunks": []byte(c.RelationChunks),
		"indirect_relation_chunks": []byte(c.IndirectRelationChunks), "metadata": []byte(c.Metadata),
		"content_hash": c.ContentHash, "image_info": c.ImageInfo, "context_header": c.ContextHeader,
		"source_locators": locators, "created_at": c.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
}

func fingerprintNativeSnapshot(doc types.Knowledge, chunks []types.Chunk) (sourceSnapshotDTO, error) {
	if doc.TenantID == 0 || doc.KnowledgeBaseID == "" || doc.ID == "" || doc.ParseStatus != "completed" || len(chunks) == 0 || len(chunks) > 4096 {
		return sourceSnapshotDTO{}, errSourceSnapshotIncomplete
	}
	size := 0
	fields, headerHash, err := hashSourceFields(nativeSourceHeader(doc), &size)
	if err != nil {
		return sourceSnapshotDTO{}, err
	}
	result := sourceSnapshotDTO{Scheme: sourceSnapshotScheme, TenantID: doc.TenantID, KnowledgeBaseID: doc.KnowledgeBaseID, DocumentID: doc.ID, HeaderFields: fields, HeaderHash: headerHash, Chunks: []sourceChunkLeaf{}}
	ordered := append([]types.Chunk(nil), chunks...)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].ChunkIndex != ordered[j].ChunkIndex {
			return ordered[i].ChunkIndex < ordered[j].ChunkIndex
		}
		return ordered[i].ID < ordered[j].ID
	})
	seen := map[string]bool{}
	for _, c := range ordered {
		if c.ID == "" || seen[c.ID] || c.TenantID != doc.TenantID || c.KnowledgeBaseID != doc.KnowledgeBaseID || c.KnowledgeID != doc.ID || c.IndexStatus != "ready" || c.ContentRevision < 0 {
			return sourceSnapshotDTO{}, errSourceSnapshotIncomplete
		}
		seen[c.ID] = true
		_, fieldsHash, e := hashSourceFields(nativeChunkFields(c), &size)
		if e != nil {
			return sourceSnapshotDTO{}, e
		}
		result.Chunks = append(result.Chunks, sourceChunkLeaf{c.ID, c.ContentRevision, c.ChunkIndex, sourceSHA([]byte(c.Content)), fieldsHash})
	}
	// This exact struct and order are the independent-module root contract.
	root := struct {
		Scheme          string            `json:"scheme"`
		TenantID        uint64            `json:"tenant_id"`
		KnowledgeBaseID string            `json:"knowledge_base_id"`
		DocumentID      string            `json:"document_id"`
		HeaderHash      string            `json:"header_hash"`
		Chunks          []sourceChunkLeaf `json:"chunks"`
	}{result.Scheme, result.TenantID, result.KnowledgeBaseID, result.DocumentID, result.HeaderHash, result.Chunks}
	encoded, _ := json.Marshal(root)
	result.RootHash = sourceSHA(encoded)
	return result, nil
}

// ExtensionSourceSnapshot returns only fingerprints from one authorized native
// read snapshot. It neither creates native versions nor mutates native rows.
func (h *KnowledgeHandler) ExtensionSourceSnapshot(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	_, kbID, tenant, _, err := h.validateKnowledgeBaseAccessWithKBID(c, c.Param("id"))
	if err != nil {
		_ = c.Error(err)
		return
	}
	fail := func(status int) { c.JSON(status, gin.H{"success": false, "error": "source_snapshot_unavailable"}) }
	componentIDs, err := sourceComponentSelection(c)
	if err != nil {
		fail(http.StatusBadRequest)
		return
	}
	if h.sourceDB == nil {
		fail(http.StatusServiceUnavailable)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 500*time.Millisecond)
	defer cancel()
	opts := &sql.TxOptions{ReadOnly: true}
	if h.sourceDB.Dialector.Name() == "postgres" {
		opts.Isolation = sql.LevelRepeatableRead
	}
	var snapshot sourceSnapshotDTO
	var components sourceComponentsDTO
	err = h.sourceDB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var doc types.Knowledge
		if e := tx.Where("tenant_id=? AND knowledge_base_id=? AND id=?", tenant, kbID, c.Param("document_id")).First(&doc).Error; e != nil {
			return e
		}
		// Bound application memory before loading chunk bodies. Both the budget
		// and the later rows are read from this same transaction snapshot.
		terms := []string{}
		for _, column := range []string{"content", "source_content", "metadata", "image_info", "context_header", "relation_chunks", "indirect_relation_chunks", "source_locators"} {
			term := "length(CAST(COALESCE(" + column + ",'') AS BLOB))"
			if tx.Dialector.Name() == "postgres" {
				term = "octet_length(COALESCE(CAST(" + column + " AS TEXT),''))"
			}
			terms = append(terms, term)
		}
		var budget struct{ ChunkCount, ByteCount int64 }
		if e := tx.Model(&types.Chunk{}).Where("tenant_id=? AND knowledge_base_id=? AND knowledge_id=?", tenant, kbID, doc.ID).
			Select("COUNT(*) AS chunk_count,COALESCE(SUM(" + strings.Join(terms, "+") + "),0) AS byte_count").Scan(&budget).Error; e != nil {
			return e
		}
		if budget.ChunkCount == 0 || budget.ChunkCount > 4096 || budget.ByteCount > 64<<20 {
			return errSourceSnapshotIncomplete
		}
		var chunks []types.Chunk
		if e := tx.Where("tenant_id=? AND knowledge_base_id=? AND knowledge_id=?", tenant, kbID, doc.ID).Order("chunk_index ASC, id ASC").Limit(4097).Find(&chunks).Error; e != nil {
			return e
		}
		var e error
		snapshot, e = fingerprintNativeSnapshot(doc, chunks)
		if e == nil && len(componentIDs) > 0 {
			components, e = fingerprintSourceComponents(snapshot, chunks, componentIDs)
		}
		return e
	}, opts)
	if err != nil {
		status := http.StatusServiceUnavailable
		if errors.Is(err, gorm.ErrRecordNotFound) {
			status = http.StatusNotFound
		} else if errors.Is(err, errSourceSnapshotIncomplete) {
			status = http.StatusConflict
		}
		fail(status)
		return
	}
	var data any = snapshot
	if len(componentIDs) > 0 {
		data = components
	}
	encoded, err := json.Marshal(gin.H{"success": true, "data": data})
	if err != nil || len(encoded) > 2<<20 {
		fail(http.StatusServiceUnavailable)
		return
	}
	c.Data(http.StatusOK, "application/json", encoded)
}
