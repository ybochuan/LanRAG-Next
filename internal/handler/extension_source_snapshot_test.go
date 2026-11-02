package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/middleware"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestSourceSnapshotSharedContract(t *testing.T) {
	_, doc, chunks := sourceSnapshotFixture(t)
	actual, err := fingerprintNativeSnapshot(doc, chunks)
	require.NoError(t, err)
	if os.Getenv("AGENTRAG_SNAPSHOT_FIXTURE_CAPTURE") == "1" {
		body, e := json.Marshal(actual)
		require.NoError(t, e)
		t.Log("SNAPSHOT_CONTRACT=" + string(body))
		return
	}
	body, err := os.ReadFile("../../contracts/bridge/v1/native-source-snapshot.json")
	require.NoError(t, err)
	var expected sourceSnapshotDTO
	require.NoError(t, json.Unmarshal(body, &expected))
	require.Equal(t, expected, actual)
}

func sourceSnapshotFixture(t *testing.T) (*gorm.DB, types.Knowledge, []types.Chunk) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "native.db")+"?_journal_mode=WAL"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { sqlDB.Close() })
	require.NoError(t, db.AutoMigrate(&types.Knowledge{}, &types.Chunk{}))
	now := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	doc := types.Knowledge{ID: "doc", TenantID: 7, KnowledgeBaseID: "kb", Type: "file", Title: "PRIVATE_TITLE", Description: "PRIVATE_DESCRIPTION", ParseStatus: "completed", CustomMetadata: types.JSON(`{"topic":"private"}`), CreatedAt: now}
	require.NoError(t, db.Create(&doc).Error)
	chunks := []types.Chunk{{ID: "chunk-a", TenantID: 7, KnowledgeBaseID: "kb", KnowledgeID: "doc", Content: "PRIVATE_BODY", ChunkType: types.ChunkTypeText, IndexStatus: "ready", ChunkIndex: 0, CreatedAt: now}, {ID: "chunk-b", TenantID: 7, KnowledgeBaseID: "kb", KnowledgeID: "doc", Content: "PRIVATE_SUMMARY", ChunkType: types.ChunkTypeSummary, IndexStatus: "ready", ChunkIndex: 1, CreatedAt: now}}
	require.NoError(t, db.Create(&chunks).Error)
	return db, doc, chunks
}

func TestSourceSnapshotFingerprintsAllContentFields(t *testing.T) {
	_, doc, chunks := sourceSnapshotFixture(t)
	original, err := fingerprintNativeSnapshot(doc, chunks)
	require.NoError(t, err)
	require.Len(t, original.Chunks, 2)
	require.Len(t, original.RootHash, 64)
	reordered, err := fingerprintNativeSnapshot(doc, []types.Chunk{chunks[1], chunks[0]})
	require.NoError(t, err)
	require.Equal(t, original.RootHash, reordered.RootHash)
	for _, change := range []func(*types.Knowledge, []types.Chunk){
		func(d *types.Knowledge, _ []types.Chunk) { d.Title += "changed" },
		func(d *types.Knowledge, _ []types.Chunk) { d.Description += "changed" },
		func(d *types.Knowledge, _ []types.Chunk) { d.CustomMetadata = types.JSON(`{"topic":"changed"}`) },
		func(_ *types.Knowledge, c []types.Chunk) { c[0].Content += "changed" },
		func(_ *types.Knowledge, c []types.Chunk) { c[1].Content += "changed" },
		func(_ *types.Knowledge, c []types.Chunk) { c[0].ContentRevision++ },
		func(_ *types.Knowledge, c []types.Chunk) { c[0].ContextHeader = "private heading" },
		func(_ *types.Knowledge, c []types.Chunk) { c[0].Metadata = types.JSON(`{"k":"v"}`) },
		func(_ *types.Knowledge, c []types.Chunk) { c[0].IsEnabled = false },
	} {
		d, c := doc, append([]types.Chunk(nil), chunks...)
		change(&d, c)
		changed, e := fingerprintNativeSnapshot(d, c)
		require.NoError(t, e)
		require.NotEqual(t, original.RootHash, changed.RootHash)
	}
	doc.UpdatedAt = time.Now()
	unchanged, err := fingerprintNativeSnapshot(doc, chunks)
	require.NoError(t, err)
	require.Equal(t, original.RootHash, unchanged.RootHash, "an update timestamp is not content provenance")
}

func TestSourceSnapshotRejectsIncompleteOrForeignData(t *testing.T) {
	_, doc, chunks := sourceSnapshotFixture(t)
	for _, status := range []string{"pending", "processing", "failed"} {
		d := doc
		d.ParseStatus = status
		_, err := fingerprintNativeSnapshot(d, chunks)
		require.Error(t, err)
	}
	_, err := fingerprintNativeSnapshot(doc, nil)
	require.Error(t, err)
	for _, change := range []func(*types.Chunk){
		func(c *types.Chunk) { c.TenantID = 8 },
		func(c *types.Chunk) { c.KnowledgeBaseID = "foreign" },
		func(c *types.Chunk) { c.KnowledgeID = "foreign" },
		func(c *types.Chunk) { c.IndexStatus = "processing" },
		func(c *types.Chunk) { c.IndexStatus = "" },
	} {
		c := append([]types.Chunk(nil), chunks...)
		change(&c[0])
		_, err = fingerprintNativeSnapshot(doc, c)
		require.Error(t, err)
	}
	_, err = fingerprintNativeSnapshot(doc, make([]types.Chunk, 4097))
	require.Error(t, err)
}

func sourceSnapshotRequest(t *testing.T, h *KnowledgeHandler, caller types.Caller, docID string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.ErrorHandler(), func(c *gin.Context) {
		ctx := context.WithValue(c.Request.Context(), types.TenantIDContextKey, caller.TenantID)
		c.Request = c.Request.WithContext(types.WithCaller(ctx, caller))
		c.Next()
	})
	r.GET("/knowledge-bases/:id/knowledge/:document_id/source-snapshot", h.ExtensionSourceSnapshot)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/knowledge-bases/kb/knowledge/"+docID+"/source-snapshot", nil))
	return w
}

func TestSourceSnapshotHTTPAuthorityReadOnlyAndFreshness(t *testing.T) {
	db, _, _ := sourceSnapshotFixture(t)
	h := &KnowledgeHandler{sourceDB: db, kbService: lifecycleKBStub{}}
	caller := types.Caller{TenantID: 7, UserID: "creator", Role: types.TenantRoleAdmin}
	require.Equal(t, 401, sourceSnapshotRequest(t, h, types.Caller{}, "doc").Code)
	response := sourceSnapshotRequest(t, h, caller, "doc")
	require.Equal(t, 200, response.Code, response.Body.String())
	require.False(t, strings.Contains(response.Body.String(), "PRIVATE_"))
	var first struct {
		Data sourceSnapshotDTO `json:"data"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &first))
	// Native manual chunk edits need not update the parent row's timestamp/hash.
	require.NoError(t, db.Model(&types.Chunk{}).Where("id=?", "chunk-a").Update("content", "changed independently").Error)
	response = sourceSnapshotRequest(t, h, caller, "doc")
	require.Equal(t, 200, response.Code)
	var second struct {
		Data sourceSnapshotDTO `json:"data"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &second))
	require.NotEqual(t, first.Data.RootHash, second.Data.RootHash)
	// Every native write operation would fail if the read endpoint attempted one.
	for name, callback := range map[string]func(string, func(*gorm.DB)) error{
		"create": func(n string, f func(*gorm.DB)) error {
			return db.Callback().Create().Before("gorm:create").Register(n, f)
		},
		"update": func(n string, f func(*gorm.DB)) error {
			return db.Callback().Update().Before("gorm:update").Register(n, f)
		},
		"delete": func(n string, f func(*gorm.DB)) error {
			return db.Callback().Delete().Before("gorm:delete").Register(n, f)
		},
	} {
		require.NoError(t, callback("snapshot-forbid-"+name, func(tx *gorm.DB) { tx.AddError(context.Canceled) }))
	}
	require.Equal(t, 200, sourceSnapshotRequest(t, h, caller, "doc").Code)
	require.Equal(t, 404, sourceSnapshotRequest(t, h, caller, "missing").Code)
}

func TestSourceSnapshotSoftDeletedChunksAndScope(t *testing.T) {
	db, _, _ := sourceSnapshotFixture(t)
	h := &KnowledgeHandler{sourceDB: db, kbService: lifecycleKBStub{}}
	caller := types.Caller{TenantID: 7, UserID: "creator", Role: types.TenantRoleAdmin}
	require.NoError(t, db.Where("knowledge_id=?", "doc").Delete(&types.Chunk{}).Error)
	require.Equal(t, 409, sourceSnapshotRequest(t, h, caller, "doc").Code)
	require.NoError(t, db.Model(&types.Knowledge{}).Where("id=?", "doc").Update("knowledge_base_id", "other").Error)
	require.Equal(t, 404, sourceSnapshotRequest(t, h, caller, "doc").Code)
}

func TestSourceSnapshotChecksRowLimitBeforeLoadingContent(t *testing.T) {
	db, _, _ := sourceSnapshotFixture(t)
	for start := 0; start < 4095; start += 100 {
		rows := []types.Chunk{}
		for i := start; i < start+100 && i < 4095; i++ {
			rows = append(rows, types.Chunk{ID: fmt.Sprint("extra-", i), TenantID: 7, KnowledgeBaseID: "kb", KnowledgeID: "doc", Content: "body", ChunkType: types.ChunkTypeText, IndexStatus: "ready", ChunkIndex: i + 2})
		}
		require.NoError(t, db.Create(&rows).Error)
	}
	loaded := false
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register("source-snapshot-content-load", func(tx *gorm.DB) {
		if tx.Statement.Table == "chunks" {
			loaded = true
		}
	}))
	h := &KnowledgeHandler{sourceDB: db, kbService: lifecycleKBStub{}}
	w := sourceSnapshotRequest(t, h, types.Caller{TenantID: 7, UserID: "creator", Role: types.TenantRoleAdmin}, "doc")
	require.Equal(t, 409, w.Code)
	require.False(t, loaded, "over-limit snapshot must not load full contents")
}

func TestSourceSnapshotConcurrentWriteUsesOneReadSnapshot(t *testing.T) {
	db, doc, chunks := sourceSnapshotFixture(t)
	before, err := fingerprintNativeSnapshot(doc, chunks)
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	var databasePath string
	row := sqlDB.QueryRow("SELECT file FROM pragma_database_list WHERE name='main'")
	require.NoError(t, row.Scan(&databasePath))
	writer, err := gorm.Open(sqlite.Open(databasePath+"?_journal_mode=WAL"), &gorm.Config{})
	require.NoError(t, err)
	writerSQL, err := writer.DB()
	require.NoError(t, err)
	defer writerSQL.Close()
	changed := false
	require.NoError(t, db.Callback().Query().After("gorm:query").Register("source-snapshot-concurrent-writer", func(tx *gorm.DB) {
		if tx.Statement.Table != "knowledges" || changed {
			return
		}
		changed = true
		err := writer.Transaction(func(w *gorm.DB) error {
			if e := w.Model(&types.Knowledge{}).Where("id=?", "doc").Update("title", "NEW_TITLE").Error; e != nil {
				return e
			}
			return w.Model(&types.Chunk{}).Where("id=?", "chunk-a").Update("content", "NEW_BODY").Error
		})
		require.NoError(t, err)
	}))
	h := &KnowledgeHandler{sourceDB: db, kbService: lifecycleKBStub{}}
	caller := types.Caller{TenantID: 7, UserID: "creator", Role: types.TenantRoleAdmin}
	response := sourceSnapshotRequest(t, h, caller, "doc")
	require.Equal(t, 200, response.Code, response.Body.String())
	var result struct {
		Data sourceSnapshotDTO `json:"data"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result))
	require.True(t, changed)
	require.Equal(t, before.RootHash, result.Data.RootHash, "must not mix old header with new chunks")
	response = sourceSnapshotRequest(t, h, caller, "doc")
	require.Equal(t, 200, response.Code)
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result))
	require.NotEqual(t, before.RootHash, result.Data.RootHash)
}
