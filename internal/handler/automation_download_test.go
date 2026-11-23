package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func newDownloadNotificationHandler(t *testing.T) (*AutomationHandler, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "download.db")+"?_busy_timeout=5000&_journal_mode=WAL"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = raw.Close() })
	require.NoError(t, db.AutoMigrate(&types.Notification{}))
	return &AutomationHandler{notifications: service.NewNotificationService(db)}, db
}

func callCreateDownloadNotification(t *testing.T, h *AutomationHandler, body string, caller *types.Caller) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/notifications/download", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	if caller != nil {
		c.Request = c.Request.WithContext(types.WithCaller(c.Request.Context(), *caller))
	}
	h.CreateDownloadNotification(c)
	return w
}

func TestCreateDownloadNotificationIsIdempotent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h, db := newDownloadNotificationHandler(t)
	caller := types.Caller{TenantID: 7, UserID: "u"}
	body := `{"file_name":"审批单.docx","source_type":"attachment","source_id":"attachment-1","source_title":"制度","link":"/platform/documents?kb=1","download_id":"download-handler-1","file_size":1024}`
	w := callCreateDownloadNotification(t, h, body, &caller)
	require.Equal(t, http.StatusOK, w.Code)
	var response struct {
		Success bool `json:"success"`
		Data    struct {
			ID        string `json:"id"`
			CreatedAt string `json:"created_at"`
			Duplicate bool   `json:"duplicate"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	require.True(t, response.Success)
	require.Equal(t, "download-handler-1", response.Data.ID)
	require.False(t, response.Data.Duplicate)
	require.NotEmpty(t, response.Data.CreatedAt)

	var row types.Notification
	require.NoError(t, db.First(&row, "id = ?", "download-handler-1").Error)
	require.Equal(t, "审批单.docx", row.Title)
	require.Equal(t, types.NotificationDownloadCompleted, row.Kind)
	require.Equal(t, "download", row.SourceType)
	require.Equal(t, "download-handler-1", row.SourceID)
	require.NotNil(t, row.ReadAt)

	duplicateBody := strings.Replace(body, "审批单.docx", "改名.docx", 1)
	w = callCreateDownloadNotification(t, h, duplicateBody, &caller)
	require.Equal(t, http.StatusOK, w.Code)
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	require.True(t, response.Data.Duplicate)
	var count int64
	require.NoError(t, db.Model(&types.Notification{}).Where("id = ?", "download-handler-1").Count(&count).Error)
	require.EqualValues(t, 1, count)

	w = callCreateDownloadNotification(t, h, body, nil)
	require.Equal(t, http.StatusUnauthorized, w.Code)

	w = callCreateDownloadNotification(t, h, strings.Replace(body, "\"attachment\"", "\"unknown\"", 1), &caller)
	require.Equal(t, http.StatusBadRequest, w.Code)
}
