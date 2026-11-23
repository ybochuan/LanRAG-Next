package service

import (
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestNotifyDownloadOnceIsIdempotentAndReadStateInsensitive(t *testing.T) {
	f := newAutomationFixture(t)
	svc := NewNotificationService(f.db)
	svc.now = func() time.Time { return f.now }

	first, created, err := svc.NotifyDownloadOnce(f.ctx, &types.Notification{
		ID: "download-1", TenantID: automationTenant, UserID: "u",
		Title: "审批单.docx", Body: "来源：制度", Kind: "ignored", SourceType: "ignored", SourceID: "ignored",
	})
	require.NoError(t, err)
	require.True(t, created)
	require.Equal(t, "download-1", first.ID)
	require.Equal(t, types.NotificationDownloadCompleted, first.Kind)
	require.Equal(t, "download", first.SourceType)
	require.Equal(t, "download-1", first.SourceID)
	require.NotNil(t, first.ReadAt)
	require.False(t, first.ReadAt.IsZero())

	duplicate, created, err := svc.NotifyDownloadOnce(f.ctx, &types.Notification{
		ID: "download-1", TenantID: automationTenant, UserID: "u", Title: "改名.docx",
	})
	require.NoError(t, err)
	require.False(t, created)
	require.Equal(t, "审批单.docx", duplicate.Title)

	var rows []types.Notification
	require.NoError(t, f.db.Where("id = ?", "download-1").Find(&rows).Error)
	require.Len(t, rows, 1)

	otherTenant, created, err := svc.NotifyDownloadOnce(f.ctx, &types.Notification{
		ID: "download-2", TenantID: automationTenant + 1, UserID: "u", Title: "另一个租户.docx",
	})
	require.NoError(t, err)
	require.True(t, created)
	require.EqualValues(t, automationTenant+1, otherTenant.TenantID)

	_, _, err = svc.NotifyDownloadOnce(f.ctx, &types.Notification{TenantID: automationTenant, UserID: "u", Title: "无ID"})
	require.Error(t, err)
}
