package attachment

import (
	"context"
	"crypto/md5"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"

	"github.com/ybochuan/LanRAG-Next/extensions/store"
	"github.com/ybochuan/LanRAG-Next/extensions/weknora"
)

func TestManagedImportRecoversCommittedAttachmentAfterLostResponse(t *testing.T) {
	ctx := t.Context()
	child := pdf()
	data := office(t, "word/document.xml", map[string][]byte{"word/embeddings/child.pdf": child})
	parentHash := fmt.Sprintf("%x", md5.Sum(data))
	var mu sync.Mutex
	var stored []map[string]any
	posts := 0
	core := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case r.URL.Path == "/api/v1/knowledge/parent":
			fmt.Fprintf(w, `{"success":true,"data":{"id":"parent","tenant_id":7,"knowledge_base_id":"kb-a","file_hash":%q,"metadata":{}}}`, parentHash)
		case r.Method == http.MethodGet:
			json.NewEncoder(w).Encode(map[string]any{"success": true, "data": stored, "total": len(stored), "page": 1, "page_size": 100})
		case r.Method == http.MethodPost:
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				t.Error(err)
				w.WriteHeader(400)
				return
			}
			defer r.MultipartForm.RemoveAll()
			var meta map[string]string
			if err := json.Unmarshal([]byte(r.FormValue("metadata")), &meta); err != nil {
				t.Error(err)
			}
			posts++
			stored = append(stored, map[string]any{"id": "child", "tenant_id": 7, "knowledge_base_id": "kb-a", "file_hash": fmt.Sprintf("%x", md5.Sum(child)), "metadata": meta})
			w.WriteHeader(http.StatusBadGateway)
		}
	}))
	defer core.Close()
	client, err := weknora.NewClient(core.URL, "fixture", 7)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "ext.db")
	db, err := store.OpenDurable(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = NewImporter(client, []string{"kb-a"}).WithSemantics(db).Process(ctx, 7, "kb-a", "parent", "parent.docx", data)
	if err == nil {
		t.Error("unconfirmed upload returned success")
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = store.OpenDurable(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for range 2 {
		receipts, err := NewImporter(client, []string{"kb-a"}).WithSemantics(db).Process(ctx, 7, "kb-a", "parent", "parent.docx", data)
		if err != nil || len(receipts) != 1 || receipts[0].DocumentID != "child" {
			t.Fatalf("recovered import = %v, %v", receipts, err)
		}
	}
	if err = db.View(ctx, func(tx *sql.Tx) error {
		var owner, hash string
		if err := tx.QueryRow(`SELECT parent_document_id,parent_file_hash FROM ext_attachment_links WHERE tenant_id='7' AND knowledge_base_id='kb-a' AND attachment_id='child'`).Scan(&owner, &hash); err != nil {
			return err
		}
		if owner != "parent" || hash != parentHash {
			t.Fatalf("persisted ownership = %q %q", owner, hash)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if posts != 1 {
		t.Fatalf("recovery submitted %d uploads", posts)
	}
}

func TestConcurrentImportReconcilesNativeReceipts(t *testing.T) {
	var mu sync.Mutex
	var stored []map[string]any
	posts := 0
	core := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.URL.Path == "/api/v1/knowledge/parent" {
			fmt.Fprint(w, `{"success":true,"data":{"id":"parent","tenant_id":7,"knowledge_base_id":"kb-a","metadata":{}}}`)
			return
		}
		if r.Method == "GET" {
			json.NewEncoder(w).Encode(map[string]any{"success": true, "data": stored, "total": len(stored), "page": 1, "page_size": 100})
			return
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		defer r.MultipartForm.RemoveAll()
		var meta map[string]string
		if err := json.Unmarshal([]byte(r.FormValue("metadata")), &meta); err != nil {
			t.Error(err)
		}
		posts++
		doc := map[string]any{"id": fmt.Sprintf("child-%d", posts), "tenant_id": 7, "knowledge_base_id": "kb-a", "metadata": meta}
		stored = append(stored, doc)
		json.NewEncoder(w).Encode(map[string]any{"success": true, "data": doc})
	}))
	defer core.Close()
	c, _ := weknora.NewClient(core.URL, "fixture", 7)
	importer := NewImporter(c, []string{"kb-a"})
	data := office(t, "word/document.xml", map[string][]byte{"word/embeddings/child.pdf": pdf()})
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			receipts, err := importer.Process(context.Background(), 7, "kb-a", "parent", "parent.docx", data)
			if err != nil || len(receipts) != 1 || receipts[0].DocumentID != "child-1" {
				t.Errorf("replayed import: %v %v", receipts, err)
			}
		})
	}
	wg.Wait()
	mu.Lock()
	defer mu.Unlock()
	if posts != 1 {
		t.Fatalf("concurrent/repeated import created %d documents", posts)
	}
}
