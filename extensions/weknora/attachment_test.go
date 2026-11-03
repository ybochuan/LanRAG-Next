package weknora

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAttachmentUploadMetadataAndDuplicateBinding(t *testing.T) {
	for _, scenario := range []string{"ok", "same_duplicate", "other_parent", "wrong_scope", "unavailable"} {
		t.Run(scenario, func(t *testing.T) {
			data := []byte("%PDF-1.4\nfixture")
			hash := sha256.Sum256(data)
			posts := 0
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("X-API-Key") != "secret" || r.Header.Get("X-Tenant-ID") != "7" {
					t.Error("missing API scope")
				}
				if r.Method == "GET" {
					io.WriteString(w, `{"success":true,"data":[],"total":0,"page":1,"page_size":100}`)
					return
				}
				posts++
				if e := r.ParseMultipartForm(1 << 20); e != nil {
					t.Fatal(e)
				}
				defer r.MultipartForm.RemoveAll()
				var meta map[string]string
				if e := json.Unmarshal([]byte(r.FormValue("metadata")), &meta); e != nil {
					t.Fatal(e)
				}
				if meta["is_attachment"] != "true" || meta["parent_document_id"] != "parent" || meta["attachment_sha256"] != hex.EncodeToString(hash[:]) {
					t.Fatal("missing forced attachment metadata")
				}
				f, h, e := r.FormFile("file")
				if e != nil {
					t.Fatal(e)
				}
				actual, _ := io.ReadAll(f)
				f.Close()
				if h.Filename != "child.pdf" || string(actual) != string(data) {
					t.Error("file bytes/name changed")
				}
				tenant := 7
				if scenario == "wrong_scope" {
					tenant = 8
				}
				if scenario == "other_parent" {
					meta["parent_document_id"] = "other"
				}
				if scenario == "unavailable" {
					w.WriteHeader(503)
					io.WriteString(w, "private contents secret")
					return
				}
				success := true
				if scenario != "ok" {
					success = false
					w.WriteHeader(409)
				}
				json.NewEncoder(w).Encode(map[string]any{"success": success, "code": "duplicate_file", "data": map[string]any{"id": "child", "tenant_id": tenant, "knowledge_base_id": "kb-a", "metadata": meta}})
			}))
			defer s.Close()
			c, _ := NewClient(s.URL, "secret", 7)
			id, e := c.UploadAttachment(context.Background(), "kb-a", "parent", "child.pdf", data)
			if scenario == "ok" || scenario == "same_duplicate" {
				if e != nil || id != "child" {
					t.Fatalf("upload: %s %v", id, e)
				}
			} else if e == nil || strings.Contains(e.Error(), "secret") {
				t.Fatalf("unsafe or accepted failure: %v", e)
			}
			if posts != 1 {
				t.Fatalf("unexpected automatic retry: %d", posts)
			}
		})
	}
}
