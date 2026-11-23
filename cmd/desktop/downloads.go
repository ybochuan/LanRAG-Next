package main

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

const (
	maxManagedDownloadBytes = 256 << 20
	downloadStoreVersion    = 1
)

// DownloadMeta is the business identity of a file the user is saving.
type DownloadMeta struct {
	FileName    string `json:"fileName"`
	SourceType  string `json:"sourceType"`
	SourceID    string `json:"sourceId"`
	SourceTitle string `json:"sourceTitle"`
	Link        string `json:"link"`
	Size        int64  `json:"size"`
}

// DownloadSession is the result of opening the native save dialog.
type DownloadSession struct {
	SessionID string `json:"sessionId"`
	FileName  string `json:"fileName"`
	Canceled  bool   `json:"canceled"`
	Fallback  bool   `json:"fallback"`
}

// DownloadRecord is one locally saved business file. It is not a copy of the
// file: SavedPath points at the user-selected location on this machine.
type DownloadRecord struct {
	ID             string    `json:"id"`
	FileName       string    `json:"fileName"`
	Size           int64     `json:"size"`
	SourceType     string    `json:"sourceType"`
	SourceID       string    `json:"sourceId"`
	SourceTitle    string    `json:"sourceTitle"`
	Link           string    `json:"link"`
	CreatedAt      time.Time `json:"createdAt"`
	Unread         bool      `json:"unread,omitempty"`
	SyncState      string    `json:"syncState"`
	SyncError      string    `json:"syncError,omitempty"`
	NotificationID string    `json:"notificationId,omitempty"`
	SavedPath      string    `json:"savedPath,omitempty"`
}

type businessDownloadSession struct {
	id          string
	fileName    string
	sourceType  string
	sourceID    string
	sourceTitle string
	link        string
	targetPath  string
	tempPath    string
	size        int64
	written     int64
	file        *os.File
}

type downloadStore struct {
	Version int              `json:"version"`
	Items   []DownloadRecord `json:"items"`
}

func (a *App) downloadsFilePath() (string, error) {
	if a != nil && a.installed != nil && strings.TrimSpace(a.installed.profile) != "" {
		return filepath.Join(a.installed.profile, "downloads.json"), nil
	}
	dir, err := desktopPrefsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "downloads.json"), nil
}

func (a *App) loadDownloadStore() downloadStore {
	store := downloadStore{Version: downloadStoreVersion}
	path, err := a.downloadsFilePath()
	if err != nil {
		return store
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return store
	}
	if json.Unmarshal(data, &store) != nil {
		return downloadStore{Version: downloadStoreVersion}
	}
	if store.Version == 0 {
		store.Version = downloadStoreVersion
	}
	store.Items = pruneDownloadRecords(store.Items)
	return store
}

func (a *App) saveDownloadStore(store downloadStore) error {
	path, err := a.downloadsFilePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	store.Version = downloadStoreVersion
	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return err
	}
	temp := path + ".tmp"
	if err := os.WriteFile(temp, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(temp, path); err != nil {
		_ = os.Remove(temp)
		return err
	}
	return nil
}

func pruneDownloadRecords(items []DownloadRecord) []DownloadRecord {
	cutoff := time.Now().UTC().Add(-30 * 24 * time.Hour)
	kept := make([]DownloadRecord, 0, len(items))
	for _, item := range items {
		if item.CreatedAt.IsZero() || item.CreatedAt.After(cutoff) {
			kept = append(kept, item)
		}
	}
	sort.Slice(kept, func(i, j int) bool { return kept[i].CreatedAt.After(kept[j].CreatedAt) })
	if len(kept) > 200 {
		kept = kept[:200]
	}
	return kept
}

// BeginBusinessDownload opens the native save dialog. A file larger than the
// managed limit is not saved here: Fallback tells the caller to use the
// browser download path without creating a record.
func (a *App) BeginBusinessDownload(meta DownloadMeta) (DownloadSession, error) {
	if a.ctx == nil {
		return DownloadSession{}, errors.New("desktop is not ready")
	}
	if strings.TrimSpace(meta.FileName) == "" {
		return DownloadSession{}, errors.New("file name is required")
	}
	if meta.Size < 0 {
		return DownloadSession{}, errors.New("invalid file size")
	}
	if meta.Size > maxManagedDownloadBytes {
		return DownloadSession{Fallback: true}, nil
	}
	if !a.pickingDownload.CompareAndSwap(false, true) {
		return DownloadSession{}, errors.New("a save dialog is already open")
	}
	defer a.pickingDownload.Store(false)
	target, err := wailsruntime.SaveFileDialog(a.ctx, wailsruntime.SaveDialogOptions{
		Title:           "保存文件",
		DefaultFilename: sanitizeBusinessFileName(meta.FileName),
		Filters:         []wailsruntime.FileFilter{{DisplayName: "所有文件", Pattern: "*.*"}},
	})
	if err != nil {
		return DownloadSession{}, err
	}
	if strings.TrimSpace(target) == "" {
		return DownloadSession{Canceled: true}, nil
	}
	if !filepath.IsAbs(target) {
		return DownloadSession{}, errors.New("invalid save target")
	}
	id := newDownloadID()
	tempPath := target + ".part-" + id
	file, err := os.OpenFile(tempPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return DownloadSession{}, err
	}
	session := &businessDownloadSession{
		id: id, fileName: filepath.Base(target),
		sourceType: meta.SourceType, sourceID: meta.SourceID, sourceTitle: meta.SourceTitle, link: meta.Link,
		targetPath: target, tempPath: tempPath, size: meta.Size, file: file,
	}
	a.downloadMu.Lock()
	a.downloadSessions[id] = session
	a.downloadMu.Unlock()
	return DownloadSession{SessionID: id, FileName: session.fileName}, nil
}

// WriteBusinessDownloadChunk appends one base64 chunk to an open session.
func (a *App) WriteBusinessDownloadChunk(sessionID, chunkBase64 string) error {
	data, err := base64.StdEncoding.DecodeString(chunkBase64)
	if err != nil {
		return errors.New("invalid download chunk")
	}
	if len(data) == 0 {
		return nil
	}
	a.downloadMu.Lock()
	defer a.downloadMu.Unlock()
	session := a.downloadSessions[sessionID]
	if session == nil {
		return errors.New("download session not found")
	}
	if session.written+int64(len(data)) > maxManagedDownloadBytes {
		return errors.New("download exceeds size limit")
	}
	if _, err := session.file.Write(data); err != nil {
		return err
	}
	session.written += int64(len(data))
	return nil
}

// FinishBusinessDownload commits the temporary file atomically and records it.
func (a *App) FinishBusinessDownload(sessionID string) (DownloadRecord, error) {
	a.downloadMu.Lock()
	session := a.downloadSessions[sessionID]
	if session == nil {
		a.downloadMu.Unlock()
		return DownloadRecord{}, errors.New("download session not found")
	}
	delete(a.downloadSessions, sessionID)
	a.downloadMu.Unlock()
	fail := func(err error) (DownloadRecord, error) {
		_ = session.file.Close()
		_ = os.Remove(session.tempPath)
		return DownloadRecord{}, err
	}
	if session.size > 0 && session.written != session.size {
		return fail(fmt.Errorf("incomplete download: got %d of %d bytes", session.written, session.size))
	}
	if err := session.file.Sync(); err != nil {
		return fail(err)
	}
	if err := session.file.Close(); err != nil {
		_ = os.Remove(session.tempPath)
		return DownloadRecord{}, err
	}
	if err := os.Remove(session.targetPath); err != nil && !os.IsNotExist(err) {
		_ = os.Remove(session.tempPath)
		return DownloadRecord{}, err
	}
	if err := os.Rename(session.tempPath, session.targetPath); err != nil {
		_ = os.Remove(session.tempPath)
		return DownloadRecord{}, err
	}
	record := DownloadRecord{
		ID: newDownloadID(), FileName: session.fileName, Size: session.written,
		SourceType: session.sourceType, SourceID: session.sourceID, SourceTitle: session.sourceTitle, Link: session.link,
		CreatedAt: time.Now().UTC(), Unread: true, SyncState: "pending", SavedPath: session.targetPath,
	}
	a.downloadRecordsMu.Lock()
	store := a.loadDownloadStore()
	store.Items = append(store.Items, record)
	err := a.saveDownloadStore(store)
	a.downloadRecordsMu.Unlock()
	if err != nil {
		return DownloadRecord{}, err
	}
	return record, nil
}

// AbortBusinessDownload discards an unfinished file.
func (a *App) AbortBusinessDownload(sessionID string) error {
	a.downloadMu.Lock()
	session := a.downloadSessions[sessionID]
	delete(a.downloadSessions, sessionID)
	a.downloadMu.Unlock()
	if session == nil {
		return nil
	}
	_ = session.file.Close()
	_ = os.Remove(session.tempPath)
	return nil
}

// ListBusinessDownloads returns records without local filesystem paths.
func (a *App) ListBusinessDownloads() []DownloadRecord {
	a.downloadRecordsMu.Lock()
	defer a.downloadRecordsMu.Unlock()
	store := a.loadDownloadStore()
	out := make([]DownloadRecord, 0, len(store.Items))
	for _, item := range store.Items {
		item.SavedPath = ""
		out = append(out, item)
	}
	return out
}

// MarkBusinessDownloadsSeen clears the local unread marker for the listed records.
func (a *App) MarkBusinessDownloadsSeen(ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	wanted := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if trimmed := strings.TrimSpace(id); trimmed != "" {
			wanted[trimmed] = struct{}{}
		}
	}
	if len(wanted) == 0 {
		return nil
	}
	a.downloadRecordsMu.Lock()
	defer a.downloadRecordsMu.Unlock()
	store := a.loadDownloadStore()
	changed := false
	for i := range store.Items {
		if _, ok := wanted[store.Items[i].ID]; !ok || !store.Items[i].Unread {
			continue
		}
		store.Items[i].Unread = false
		changed = true
	}
	if !changed {
		return nil
	}
	return a.saveDownloadStore(store)
}

func (a *App) MarkBusinessDownloadSynced(id, notificationID string) error {
	if strings.TrimSpace(id) == "" {
		return errors.New("download id is required")
	}
	a.downloadRecordsMu.Lock()
	defer a.downloadRecordsMu.Unlock()
	store := a.loadDownloadStore()
	found := false
	for i := range store.Items {
		if store.Items[i].ID == id {
			store.Items[i].SyncState = "synced"
			store.Items[i].SyncError = ""
			store.Items[i].NotificationID = strings.TrimSpace(notificationID)
			found = true
			break
		}
	}
	if !found {
		return errors.New("download record not found")
	}
	return a.saveDownloadStore(store)
}

func (a *App) MarkBusinessDownloadFailed(id, message string) error {
	if strings.TrimSpace(id) == "" {
		return errors.New("download id is required")
	}
	if r := []rune(message); len(r) > 500 {
		message = string(r[:500])
	}
	a.downloadRecordsMu.Lock()
	defer a.downloadRecordsMu.Unlock()
	store := a.loadDownloadStore()
	found := false
	for i := range store.Items {
		if store.Items[i].ID == id {
			store.Items[i].SyncState = "failed"
			store.Items[i].SyncError = message
			found = true
			break
		}
	}
	if !found {
		return errors.New("download record not found")
	}
	return a.saveDownloadStore(store)
}

func (a *App) lookupBusinessDownload(id string) (DownloadRecord, error) {
	a.downloadRecordsMu.Lock()
	defer a.downloadRecordsMu.Unlock()
	store := a.loadDownloadStore()
	for _, item := range store.Items {
		if item.ID == id {
			if strings.TrimSpace(item.SavedPath) == "" {
				return DownloadRecord{}, errors.New("saved file is missing")
			}
			info, err := os.Stat(item.SavedPath)
			if err != nil || !info.Mode().IsRegular() {
				return DownloadRecord{}, errors.New("saved file is missing")
			}
			return item, nil
		}
	}
	return DownloadRecord{}, errors.New("download record not found")
}

// OpenBusinessDownload opens only a path previously chosen through the dialog.
func (a *App) OpenBusinessDownload(id string) error {
	record, err := a.lookupBusinessDownload(id)
	if err != nil {
		return err
	}
	switch runtime.GOOS {
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", record.SavedPath).Start()
	case "darwin":
		return exec.Command("open", record.SavedPath).Start()
	default:
		return exec.Command("xdg-open", record.SavedPath).Start()
	}
}

// RevealBusinessDownload selects the saved file in the system file manager.
func (a *App) RevealBusinessDownload(id string) error {
	record, err := a.lookupBusinessDownload(id)
	if err != nil {
		return err
	}
	switch runtime.GOOS {
	case "windows":
		return exec.Command("explorer.exe", "/select,"+record.SavedPath).Start()
	case "darwin":
		return exec.Command("open", "-R", record.SavedPath).Start()
	default:
		return exec.Command("xdg-open", filepath.Dir(record.SavedPath)).Start()
	}
}

func newDownloadID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("dl-%d", time.Now().UnixNano())
	}
	return "dl-" + hex.EncodeToString(b[:])
}

func sanitizeBusinessFileName(raw string) string {
	name := strings.TrimSpace(strings.ReplaceAll(raw, "\\", "/"))
	name = filepath.Base(name)
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, name)
	name = strings.TrimRight(name, ". ")
	if name == "" || name == "." || name == ".." {
		return "download"
	}
	runes := []rune(name)
	if len(runes) > 200 {
		ext := []rune(filepath.Ext(name))
		keep := 200 - len(ext)
		if keep < 1 {
			keep, ext = 200, nil
		}
		name = string(runes[:keep]) + string(ext)
	}
	return name
}
