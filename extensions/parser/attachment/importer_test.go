package attachment

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/ybochuan/LanRAG-Next/extensions/weknora"
)

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
