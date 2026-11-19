package attachmentworkbench

import (
	"crypto/md5"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/ybochuan/LanRAG-Next/extensions/parser/attachment"
	"github.com/ybochuan/LanRAG-Next/extensions/store"
	"github.com/ybochuan/LanRAG-Next/extensions/weknora"
)

func TestRealDOCXAttachmentWorkbench(t *testing.T) {
	path := os.Getenv("AGENTRAG_ATTACHMENT_SAMPLE")
	if path == "" {
		t.Skip("no local real DOCX sample configured")
	}
	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	const kb, parentID = "fixture-kb", "parent"
	hash := fmt.Sprintf("%x", md5.Sum(source))
	parent := map[string]any{"id": parentID, "tenant_id": 7, "knowledge_base_id": kb, "title": "主件", "file_name": "source.docx",
		"file_hash": hash, "parse_status": "completed", "enable_status": "enabled", "metadata": map[string]string{}}
	children := map[string]map[string]any{}
	var mu sync.Mutex
	posts := 0
	core := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		send := func(data any) { _ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": data}) }
		switch {
		case r.URL.Path == "/api/v1/knowledge/parent/download":
			_, _ = w.Write(source)
		case r.URL.Path == "/api/v1/knowledge/parent":
			send(parent)
		case r.URL.Path == "/api/v1/knowledge/batch":
			rows := []map[string]any{}
			for _, id := range r.URL.Query()["ids"] {
				if id == parentID {
					rows = append(rows, parent)
				} else if d := children[id]; d != nil {
					rows = append(rows, d)
				}
			}
			send(rows)
		case r.Method == http.MethodPut:
			id := strings.TrimPrefix(r.URL.Path, "/api/v1/knowledge/")
			var req map[string]string
			row := children[id]
			if json.NewDecoder(r.Body).Decode(&req) != nil || row == nil || row["title"] != req["expected_title"] || row["file_hash"] != req["expected_file_hash"] {
				w.WriteHeader(http.StatusConflict)
				return
			}
			row["title"] = req["title"]
			send(row)
		case r.Method == http.MethodPost:
			file, head, err := r.FormFile("file")
			if err != nil {
				t.Error("missing native upload bytes")
				w.WriteHeader(400)
				return
			}
			defer file.Close()
			defer r.MultipartForm.RemoveAll()
			data, err := io.ReadAll(file)
			if err != nil || r.FormValue("fileName") != weknora.AttachmentFolder+"/"+head.Filename {
				t.Error("native file or folder was not preserved")
				w.WriteHeader(400)
				return
			}
			var meta map[string]string
			_ = json.Unmarshal([]byte(r.FormValue("metadata")), &meta)
			posts++
			id := fmt.Sprintf("child-%d", posts)
			children[id] = map[string]any{"id": id, "tenant_id": 7, "knowledge_base_id": kb, "title": head.Filename,
				"file_name": head.Filename, "folder_path": weknora.AttachmentFolder, "file_hash": fmt.Sprintf("%x", md5.Sum(data)),
				"parse_status": "completed", "enable_status": "enabled", "metadata": meta}
			send(children[id])
		default:
			rows := []map[string]any{parent}
			for _, d := range children {
				rows = append(rows, d)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": rows, "total": len(rows), "page": 1, "page_size": 100})
		}
	}))
	defer core.Close()
	client, err := weknora.NewClient(core.URL, "fixture", 7)
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	db, err := store.OpenDurable(ctx, filepath.Join(t.TempDir(), "extension.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	importer := attachment.NewImporter(client, []string{kb}).WithSemantics(db)
	s := New(db, client, importer)
	scope := Scope{Tenant: 7, KB: kb}
	preview, err := s.Preview(ctx, scope, parentID)
	if err != nil || len(preview.Preview) != 6 || len(preview.References) == 0 {
		t.Fatalf("preview: files=%d references=%d err=%v", len(preview.Preview), len(preview.References), err)
	}
	selection := []string{preview.Preview[0].Fingerprint}
	command := Command{Parent: parentID, ParentVersion: hash, Fingerprints: selection}
	for range 2 {
		if receipts, err := s.Import(ctx, scope, command); err != nil || len(receipts) != 1 {
			t.Fatalf("selected import: count=%d err=%v", len(receipts), err)
		}
	}
	list, err := s.List(ctx, scope, parentID)
	if err != nil || len(list.Items) != 1 || !list.Items[0].Managed || list.Items[0].Folder != weknora.AttachmentFolder {
		t.Fatalf("native workbench view: count=%d err=%v", len(list.Items), err)
	}
	item := list.Items[0]
	command = Command{Parent: parentID, ParentVersion: hash, Document: item.ID, DocumentVersion: item.Version,
		PreviousTitle: item.NativeTitle, Title: "已确认的验收附件", ReferenceID: &preview.References[0].ID}
	for range 2 {
		confirmed, err := s.Edit(ctx, scope, "confirm", "7:editor", "confirm-sample", 0, command)
		if err != nil || confirmed.Revision != 1 || confirmed.TitleState != "synced" {
			t.Fatalf("confirm result: %+v %v", confirmed, err)
		}
	}
	if _, err := s.Edit(ctx, scope, "confirm", "7:editor", "stale-save", 0, command); !errors.Is(err, store.ErrAttachmentConflict) {
		t.Fatalf("stale revision: %v", err)
	}
	if _, err := importer.Process(ctx, 7, kb, parentID, "source.docx", source); err != nil {
		t.Fatal(err)
	}
	list, err = s.List(ctx, scope, parentID)
	if err != nil || len(list.Items) != 6 {
		t.Fatalf("re-extract: count=%d err=%v", len(list.Items), err)
	}
	for _, row := range list.Items {
		if row.ID == item.ID && (row.Mapping != "manual" || row.NativeTitle != command.Title || row.Revision != 1) {
			t.Fatal("re-extraction overwrote the manual decision")
		}
	}
	if _, err := s.Edit(ctx, scope, "detach", "7:editor", "detach-sample", 1, command); err != nil {
		t.Fatal(err)
	}
	if _, err := importer.Process(ctx, 7, kb, parentID, "source.docx", source); err != nil {
		t.Fatal(err)
	}
	list, err = s.List(ctx, scope, parentID)
	if err != nil || len(list.Items) != 6 {
		t.Fatalf("read after detach: count=%d err=%v", len(list.Items), err)
	}
	for _, row := range list.Items {
		if row.ID == item.ID && row.State != "detached" {
			t.Fatal("same-version import restored a detached attachment")
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if posts != 6 {
		t.Fatalf("native upload count=%d, want 6", posts)
	}
	t.Logf("verified six real annexes, %d reference choices, current title write and same-version suppression", len(preview.References))
}
