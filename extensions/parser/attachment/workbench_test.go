package attachment

import (
	"crypto/md5"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"

	"github.com/ybochuan/LanRAG-Next/extensions/store"
	"github.com/ybochuan/LanRAG-Next/extensions/weknora"
)

type importFixture struct {
	mu       sync.Mutex
	data     []byte
	db       *store.DB
	importer *Importer
	stored   map[string]map[string]any
	posts    map[string]int
	reject   map[string]int
}

func newImportFixture(t *testing.T, data []byte) *importFixture {
	t.Helper()
	f := &importFixture{data: data, stored: map[string]map[string]any{}, posts: map[string]int{}, reject: map[string]int{}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		send := func(data any) { _ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": data}) }
		parent := map[string]any{"id": "parent", "tenant_id": 7, "knowledge_base_id": "kb", "file_name": "parent.docx", "file_hash": fmt.Sprintf("%x", md5.Sum(f.data)), "metadata": map[string]string{}}
		switch {
		case r.URL.Path == "/api/v1/knowledge/parent/download":
			_, _ = w.Write(f.data)
		case r.URL.Path == "/api/v1/knowledge/parent":
			send(parent)
		case r.URL.Path == "/api/v1/knowledge/batch":
			send([]any{parent})
		case r.Method == http.MethodGet:
			rows := []map[string]any{}
			for _, row := range f.stored {
				rows = append(rows, row)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": rows, "total": len(rows), "page": 1, "page_size": 100})
		case r.Method == http.MethodPost:
			file, header, err := r.FormFile("file")
			if err != nil {
				t.Error(err)
				w.WriteHeader(400)
				return
			}
			defer file.Close()
			defer r.MultipartForm.RemoveAll()
			f.posts[header.Filename]++
			if status := f.reject[header.Filename]; status != 0 {
				f.reject[header.Filename] = 0
				w.WriteHeader(status)
				return
			}
			data, err := io.ReadAll(file)
			if err != nil {
				t.Error(err)
				w.WriteHeader(400)
				return
			}
			var metadata map[string]string
			if err = json.Unmarshal([]byte(r.FormValue("metadata")), &metadata); err != nil {
				t.Error(err)
				w.WriteHeader(400)
				return
			}
			id := "file-" + sha256Hex(data)[:12]
			row := map[string]any{"id": id, "tenant_id": 7, "knowledge_base_id": "kb", "file_name": header.Filename, "file_hash": fmt.Sprintf("%x", md5.Sum(data)), "metadata": metadata}
			f.stored[id] = row
			send(row)
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(server.Close)
	client, err := weknora.NewClient(server.URL, "fixture", 7)
	if err != nil {
		t.Fatal(err)
	}
	f.db, err = store.OpenDurable(t.Context(), filepath.Join(t.TempDir(), "extension.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.db.Close() })
	f.importer = NewImporter(client, []string{"kb"}).WithSemantics(f.db)
	return f
}

func TestIncompleteManifestPreservesOldAttachmentsAndCompleteEmptyRetiresThem(t *testing.T) {
	f := newImportFixture(t, office(t, "word/document.xml", map[string][]byte{"word/embeddings/child.pdf": pdf()}))
	if receipts, err := f.importer.Process(t.Context(), 7, "kb", "parent", "parent.docx", f.data); err != nil || len(receipts) != 1 {
		t.Fatalf("initial import = %v %v", receipts, err)
	}
	for _, tc := range []struct {
		contents map[string][]byte
		retired  bool
	}{
		{map[string][]byte{"word/embeddings/child.pdf": []byte("invalid container")}, false},
		{map[string][]byte{}, true},
	} {
		f.mu.Lock()
		f.data = office(t, "word/document.xml", tc.contents)
		f.mu.Unlock()
		if _, err := f.importer.Process(t.Context(), 7, "kb", "parent", "parent.docx", f.data); err != nil {
			t.Fatal(err)
		}
		if err := f.db.View(t.Context(), func(tx *sql.Tx) error {
			var retired bool
			if err := tx.QueryRow(`SELECT retired_at IS NOT NULL FROM ext_attachment_links`).Scan(&retired); err != nil {
				return err
			}
			if retired != tc.retired {
				t.Fatalf("retired=%v, want %v", retired, tc.retired)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSelectedImportReturnsEveryOutcomeAndRetriesOnlyRejectedFile(t *testing.T) {
	f := newImportFixture(t, office(t, "word/document.xml", map[string][]byte{"word/embeddings/ok.pdf": pdf(), "word/embeddings/retry.pdf": append(pdf(), []byte("\n%retry")...)}))
	f.reject["retry.pdf"] = http.StatusTooManyRequests
	hash := fmt.Sprintf("%x", md5.Sum(f.data))
	preview, _, issues, err := f.importer.Preview(t.Context(), 7, "kb", "parent", hash)
	if err != nil || len(preview) != 2 || len(issues) != 0 {
		t.Fatalf("preview = %v %v %v", preview, issues, err)
	}
	selected := []string{preview[0].Fingerprint, preview[1].Fingerprint}
	outcomes, err := f.importer.ImportSelected(t.Context(), 7, "kb", "parent", hash, selected)
	if err != nil || len(outcomes) != 2 {
		t.Fatalf("outcomes = %+v %v", outcomes, err)
	}
	failed := ""
	for _, item := range outcomes {
		if item.FileName == "retry.pdf" {
			if item.Error != "rejected" || item.DocumentID != "" {
				t.Fatalf("rejected outcome = %+v", item)
			}
			failed = item.Fingerprint
		} else if item.DocumentID == "" || item.Error != "" {
			t.Fatalf("successful outcome = %+v", item)
		}
	}
	if failed == "" {
		t.Fatal("missing rejected item")
	}
	outcomes, err = f.importer.ImportSelected(t.Context(), 7, "kb", "parent", hash, []string{failed})
	if err != nil || len(outcomes) != 1 || outcomes[0].DocumentID == "" || outcomes[0].Error != "" {
		t.Fatalf("retry = %+v %v", outcomes, err)
	}
	if f.posts["ok.pdf"] != 1 || f.posts["retry.pdf"] != 2 {
		t.Fatalf("native posts = %v", f.posts)
	}
}
