package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestFinishBusinessDownloadMarksRecordUnread(t *testing.T) {
	dir := t.TempDir()
	app := &App{
		installed:        &installedDesktop{profile: dir},
		downloadSessions: map[string]*businessDownloadSession{},
	}
	target := filepath.Join(dir, "saved.txt")
	temp := filepath.Join(dir, "saved.part")
	require.NoError(t, os.WriteFile(temp, []byte("hello"), 0o600))
	file, err := os.OpenFile(temp, os.O_RDWR, 0)
	require.NoError(t, err)
	app.downloadSessions["session-1"] = &businessDownloadSession{
		id: "session-1", fileName: "saved.txt", sourceType: "knowledge_document",
		sourceID: "doc-1", sourceTitle: "Test document", link: "/platform/knowledge-bases/test",
		targetPath: target, tempPath: temp, size: 5, written: 5, file: file,
	}

	record, err := app.FinishBusinessDownload("session-1")
	require.NoError(t, err)
	require.True(t, record.Unread)
	require.FileExists(t, target)
	records := app.ListBusinessDownloads()
	require.Len(t, records, 1)
	require.True(t, records[0].Unread)
	require.Empty(t, records[0].SavedPath, "the list API must not expose local paths")
}

func TestMarkBusinessDownloadsSeenOnlyClearsRequestedRecords(t *testing.T) {
	dir := t.TempDir()
	app := &App{installed: &installedDesktop{profile: dir}}
	now := time.Now().UTC()
	require.NoError(t, app.saveDownloadStore(downloadStore{Items: []DownloadRecord{
		{ID: "read-me", FileName: "a.txt", CreatedAt: now, Unread: true, SyncState: "synced"},
		{ID: "keep-unread", FileName: "b.txt", CreatedAt: now, Unread: true, SyncState: "synced"},
	}}))

	require.NoError(t, app.MarkBusinessDownloadsSeen([]string{" read-me ", "missing"}))
	records := app.ListBusinessDownloads()
	require.Len(t, records, 2)
	byID := map[string]DownloadRecord{}
	for _, record := range records {
		byID[record.ID] = record
	}
	require.False(t, byID["read-me"].Unread)
	require.True(t, byID["keep-unread"].Unread)
}

func TestLegacyDownloadRecordWithoutUnreadIsRead(t *testing.T) {
	dir := t.TempDir()
	app := &App{installed: &installedDesktop{profile: dir}}
	require.NoError(t, os.WriteFile(filepath.Join(dir, "downloads.json"),
		[]byte(`{"version":1,"items":[{"id":"legacy","fileName":"old.txt","syncState":"synced"}]}`), 0o600))

	records := app.ListBusinessDownloads()
	require.Len(t, records, 1)
	require.False(t, records[0].Unread)
	require.NoError(t, app.MarkBusinessDownloadsSeen([]string{"legacy"}))
}
