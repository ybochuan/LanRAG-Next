package handler

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestSourceComponentViewBindsEveryFieldToExistingRoot(t *testing.T) {
	_, doc, chunks := sourceSnapshotFixture(t)
	v1, e := fingerprintNativeSnapshot(doc, chunks)
	require.NoError(t, e)
	view, e := fingerprintSourceComponents(v1, chunks, []string{"chunk-a"})
	require.NoError(t, e)
	require.Equal(t, v1, view.Snapshot)
	size := 0
	fields, folded, e := hashSourceFields(nativeChunkFields(chunks[0]), &size)
	require.NoError(t, e)
	require.Equal(t, v1.Chunks[0].FieldsHash, folded)
	require.Equal(t, fields, view.ChunkFields["chunk-a"])
	body, e := json.Marshal(view)
	require.NoError(t, e)
	require.NotContains(t, string(body), "PRIVATE_")
	_, e = fingerprintSourceComponents(v1, chunks, []string{"foreign"})
	require.Error(t, e)
	changed := append([]types.Chunk(nil), chunks...)
	changed[0].Content += "changed after root"
	_, e = fingerprintSourceComponents(v1, changed, []string{"chunk-a"})
	require.Error(t, e)
}

func TestSourceComponentQueryRequiresExplicitBoundedSelection(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, query := range []string{"view=other", "view=components-v1", "chunk_ids=chunk-a", "view=components-v1&chunk_ids=chunk-a,chunk-a", "view=components-v1&chunk_ids=../foreign", "view=components-v1&chunk_ids=" + strings.Repeat("a,", 64) + "b"} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("GET", "/?"+query, nil)
		_, e := sourceComponentSelection(c)
		require.Error(t, e, query)
	}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/?view=components-v1&chunk_ids=chunk-a,chunk-b", nil)
	ids, e := sourceComponentSelection(c)
	require.NoError(t, e)
	require.Equal(t, []string{"chunk-a", "chunk-b"}, ids)
}
