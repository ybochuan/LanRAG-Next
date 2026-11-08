package main

import (
	"archive/zip"
	"bytes"
	"crypto/md5"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/ybochuan/LanRAG-Next/extensions/store"
)

func TestLegacyPlanRevalidatesBytesAndAppliesIdempotently(t *testing.T) {
	t.Run("direct local path", func(t *testing.T) { testLegacyPlan(t, false) })
	t.Run("native resource binding", func(t *testing.T) { testLegacyPlan(t, true) })
}

func testLegacyPlan(t *testing.T, resource bool) {
	t.Helper()
	ctx := t.Context()
	root := t.TempDir()
	nativePath, extPath := filepath.Join(root, "native.db"), filepath.Join(root, "ext.db")
	child := []byte("%PDF-1.7\nlegacy attachment")
	var main bytes.Buffer
	z := zip.NewWriter(&main)
	for name, data := range map[string][]byte{
		"[Content_Types].xml":       []byte("<Types/>"),
		"word/document.xml":         []byte(`<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>Sample</w:t></w:r></w:p></w:body></w:document>`),
		"word/embeddings/child.pdf": child,
	} {
		w, err := z.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = w.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	write := func(path string, data []byte) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	childPath := filepath.Join(root, "7", "child", "table.pdf")
	write(childPath, child)
	write(filepath.Join(root, "7", "parent", "policy.docx"), main.Bytes())
	native, err := sql.Open("sqlite", nativePath)
	if err != nil {
		t.Fatal(err)
	}
	defer native.Close()
	if _, err = native.Exec(`CREATE TABLE knowledges(id TEXT PRIMARY KEY, tenant_id INTEGER, knowledge_base_id TEXT,
		type TEXT, file_name TEXT, file_path TEXT, file_hash TEXT, parse_status TEXT, enable_status TEXT, metadata TEXT, deleted_at TEXT)`); err != nil {
		t.Fatal(err)
	}
	for _, entry := range []struct{ id, name, hash, metadata string }{
		{"parent", "policy.docx", fmt.Sprintf("%x", md5.Sum(main.Bytes())), "{}"},
		{"child", "table.pdf", fmt.Sprintf("%x", md5.Sum(child)), fmt.Sprintf(`{"is_attachment":"true","parent_document_id":"parent","attachment_sha256":"%x"}`, sha256.Sum256(child))},
	} {
		if _, err = native.Exec(`INSERT INTO knowledges VALUES(?,7,'kb','file',?,?,?,'completed','enabled',?,NULL)`,
			entry.id, entry.name, "local://7/"+entry.id+"/"+entry.name, entry.hash, entry.metadata); err != nil {
			t.Fatal(err)
		}
	}
	if resource {
		_, err = native.Exec(`
			CREATE TABLE resources(id TEXT, handle TEXT, tenant_id INTEGER, storage_backend_id TEXT, provider TEXT, physical_path TEXT, state TEXT, deleted_at TEXT);
			CREATE TABLE resource_bindings(resource_id TEXT, tenant_id INTEGER, owner_type TEXT, owner_id TEXT, relation TEXT);
			CREATE TABLE storage_backends(id TEXT, tenant_id INTEGER, provider TEXT, status TEXT, config TEXT, deleted_at TEXT);
			INSERT INTO storage_backends VALUES('backend',7,'local','active','{}',NULL);
			INSERT INTO resources VALUES
				('r1','AbCdEfGhIjKlMnOpQrStUv',7,'backend','local','storage://backend/local://7/parent/policy.docx','active',NULL),
				('r2','ZyXwVuTsRqPoNmLkJiHgFe',7,'backend','local','storage://backend/local://7/child/table.pdf','active',NULL);
			INSERT INTO resource_bindings VALUES('r1',7,'knowledge','wrong-parent','source_file'),('r2',7,'knowledge','child','source_file');
			UPDATE knowledges SET file_path='resource://AbCdEfGhIjKlMnOpQrStUv' WHERE id='parent';
			UPDATE knowledges SET file_path='resource://ZyXwVuTsRqPoNmLkJiHgFe' WHERE id='child';`)
		if err != nil {
			t.Fatal(err)
		}
		var unbound bytes.Buffer
		if err = legacyOwnership(ctx, "", nativePath, root, 7, "kb", "", &unbound); err != nil {
			t.Fatal(err)
		}
		var rejected legacyOwnershipPlan
		if err = json.Unmarshal(unbound.Bytes(), &rejected); err != nil || len(rejected.Entries) != 0 || len(rejected.Skipped) != 1 {
			t.Fatalf("resource without the matching source binding: %+v, %v", rejected, err)
		}
		if _, err = native.Exec(`UPDATE resource_bindings SET owner_id='parent' WHERE resource_id='r1'`); err != nil {
			t.Fatal(err)
		}
	}
	ext, err := store.OpenDurable(ctx, extPath)
	if err != nil {
		t.Fatal(err)
	}
	if err = ext.Close(); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err = legacyOwnership(ctx, "", nativePath, root, 7, "kb", "", &output); err != nil {
		t.Fatal(err)
	}
	var plan legacyOwnershipPlan
	if err = json.Unmarshal(output.Bytes(), &plan); err != nil || len(plan.Entries) != 1 {
		t.Fatalf("legacy plan = %+v, %v", plan, err)
	}
	planPath := filepath.Join(root, "plan.json")
	write(planPath, output.Bytes())
	write(childPath, []byte("changed bytes"))
	if err = legacyOwnership(ctx, extPath, nativePath, root, 7, "kb", planPath, &output); err == nil {
		t.Fatal("apply trusted a plan after native attachment bytes changed")
	}
	write(childPath, child)
	for range 2 {
		if err = legacyOwnership(ctx, extPath, nativePath, root, 7, "kb", planPath, &output); err != nil {
			t.Fatal(err)
		}
	}
	reader, err := store.OpenReadOnly(ctx, extPath)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	var n int
	if err = reader.QueryRow(`SELECT COUNT(*) FROM ext_attachment_links WHERE evidence_kind='legacy_reextracted'
		AND attachment_id='child' AND adoption_parent_sha256=?`, plan.Entries[0].ParentSHA256).Scan(&n); err != nil || n != 1 {
		t.Fatalf("legacy proof count = %d, %v", n, err)
	}
}
