package parser

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"

	"github.com/ybochuan/LanRAG-Next/extensions/parser/attachment"
	"github.com/ybochuan/LanRAG-Next/extensions/weknora"
)

func attachmentOffice(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	for name, text := range map[string]string{"[Content_Types].xml": `<Types/>`, "word/document.xml": `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>main</w:t></w:r></w:p><w:p><w:r><w:t>附件1</w:t></w:r></w:p><w:p><w:r><w:t>annex content</w:t></w:r></w:p></w:body></w:document>`} {
		w, e := z.Create(name)
		if e != nil {
			t.Fatal(e)
		}
		w.Write([]byte(text))
	}
	z.Close()
	return b.Bytes()
}

func TestNativeAttachmentEndpointIdentityAndOneLevelImport(t *testing.T) {
	for _, scenario := range []string{"normal", "child", "bad_auth", "bad_kb", "parent_scope"} {
		t.Run(scenario, func(t *testing.T) {
			var posts atomic.Int32
			core := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "GET" && r.URL.Path == "/api/v1/knowledge/parent" {
					kb := "kb-a"
					if scenario == "parent_scope" {
						kb = "kb-b"
					}
					meta := map[string]string{}
					if scenario == "child" {
						meta["is_attachment"] = "true"
					}
					json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{"id": "parent", "tenant_id": 7, "knowledge_base_id": kb, "metadata": meta}})
					return
				}
				if r.Method == "GET" {
					fmt.Fprint(w, `{"success":true,"data":[],"total":0,"page":1,"page_size":100}`)
					return
				}
				posts.Add(1)
				r.ParseMultipartForm(1 << 20)
				defer r.MultipartForm.RemoveAll()
				var meta map[string]string
				json.Unmarshal([]byte(r.FormValue("metadata")), &meta)
				json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{"id": "child", "tenant_id": 7, "knowledge_base_id": "kb-a", "metadata": meta}})
			}))
			defer core.Close()
			client, _ := weknora.NewClient(core.URL, "fixture", 7)
			h, e := NewNativeHandler(NativeConfig{UpstreamURL: core.URL, APIKey: nativeTestKey, ArtifactRoot: t.TempDir(), Scopes: []ScopeIdentity{{7, "kb-a"}}, Attachments: attachment.NewImporter(client, []string{"kb-a"})})
			if e != nil {
				t.Fatal(e)
			}
			r := httptest.NewRequest("POST", "/parser/native/v1/attachments", bytes.NewReader(attachmentOffice(t)))
			r.Header.Set("Authorization", "Bearer "+nativeTestKey)
			r.Header.Set("X-AgentRAG-Bridge-Version", "1")
			r.Header.Set("X-AgentRAG-Tenant-ID", "7")
			r.Header.Set("X-AgentRAG-KB-ID", "kb-a")
			r.Header.Set("X-AgentRAG-Doc-ID", "parent")
			r.Header.Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": "parent.docx"}))
			if scenario == "bad_auth" {
				r.Header.Set("Authorization", "Bearer denied")
			}
			if scenario == "bad_kb" {
				r.Header.Set("X-AgentRAG-KB-ID", "kb-b")
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if scenario == "normal" {
				if w.Code != 200 || posts.Load() != 1 {
					t.Fatalf("import HTTP %d, posts %d", w.Code, posts.Load())
				}
			} else if scenario == "child" {
				if w.Code != 200 || posts.Load() != 0 {
					t.Fatal("recursive attachment import")
				}
			} else if w.Code == 200 || posts.Load() != 0 {
				t.Fatalf("invalid scope accepted: %d", w.Code)
			}
		})
	}
}

func TestNativePDFImportsBeforePreflight(t *testing.T) {
	f := fakeMinerUFixture(t)
	var imports atomic.Int32
	core := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/knowledge/doc-a" {
			fmt.Fprint(w, `{"success":true,"data":{"id":"doc-a","tenant_id":7,"knowledge_base_id":"kb-a","metadata":{}}}`)
			return
		}
		if r.Method == "GET" {
			fmt.Fprint(w, `{"success":true,"data":[],"total":0,"page":1,"page_size":100}`)
			return
		}
		r.ParseMultipartForm(1 << 20)
		defer r.MultipartForm.RemoveAll()
		var meta map[string]string
		json.Unmarshal([]byte(r.FormValue("metadata")), &meta)
		imports.Add(1)
		json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{"id": "child", "tenant_id": 7, "knowledge_base_id": "kb-a", "metadata": meta}})
	}))
	defer core.Close()
	client, _ := weknora.NewClient(core.URL, "fixture", 7)
	h, err := NewNativeHandler(NativeConfig{UpstreamURL: f.server.URL, UpstreamKey: testBackendKey, APIKey: nativeTestKey, ArtifactRoot: t.TempDir(), Scopes: []ScopeIdentity{{7, "kb-a"}}, Attachments: attachment.NewImporter(client, []string{"kb-a"})})
	if err != nil {
		t.Fatal(err)
	}
	// A reader supplies the real embedded file bytes without filesystem writes.
	child := attachmentOffice(t)
	var source bytes.Buffer
	cfg := model.NewStatelessConfiguration()
	// pdfcpu AddAttachments accepts paths, so keep its synthetic input in TempDir.
	name := filepath.Join(t.TempDir(), "child.docx")
	if err := os.WriteFile(name, child, 0600); err != nil {
		t.Fatal(err)
	}
	if err := api.AddAttachments(context.Background(), bytes.NewReader(testPDF()), &source, []string{name}, false, cfg); err != nil {
		t.Fatal(err)
	}
	meta := sourceMetadata(source.Bytes())
	created := nativeCall(t, h, "POST", "/parser/native/v1/uploads", "doc-a", meta)
	if created.status != 200 {
		t.Fatalf("create HTTP %d", created.status)
	}
	reply := nativeCall(t, h, "PUT", created.object(t)["upload_url"].(string), "doc-a", source.Bytes())
	if reply.status != 204 || imports.Load() != 1 {
		t.Fatalf("PDF preflight/upload: HTTP %d, imports %d", reply.status, imports.Load())
	}
}
