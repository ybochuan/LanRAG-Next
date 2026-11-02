package parser

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func middleZIP(t *testing.T, names ...string) []byte {
	t.Helper()
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	for _, name := range names {
		w, err := z.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(`{"schema_version":"2.0","pages":[],"extensions":{"test":true}}`)); err != nil {
			t.Fatal(err)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestArtifactWritesOriginalMiddleJSONAtomicallyAndRefusesConflicts(t *testing.T) {
	root := filepath.Join(t.TempDir(), "data", "parser_artifacts")
	a := &artifactStore{path: root}
	ref := artifactRef{DocumentID: "doc-a", SourceFileID: "f-a", ScopeIdentity: ScopeIdentity{TenantID: 7, KnowledgeBaseID: "kb-a"}, MinerUVersion: "4.0.1"}
	archive := middleZIP(t, "middle_json.json")
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); errs <- a.persist(ref, archive) }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	b, err := os.ReadFile(filepath.Join(root, "doc-a", "middle.json"))
	if err != nil || !bytes.Equal(b, []byte(`{"schema_version":"2.0","pages":[],"extensions":{"test":true}}`)) {
		t.Fatalf("middle not preserved: %v", err)
	}
	ref.KnowledgeBaseID = "kb-b"
	if err := a.persist(ref, archive); err == nil {
		t.Fatal("another scope replaced a document artifact")
	}
	ref.KnowledgeBaseID = "kb-a"
	ref.SourceFileID = "other-source"
	if err := a.persist(ref, archive); err == nil {
		t.Fatal("conflicting source replaced artifact")
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 1 {
		t.Fatalf("unexpected staging leftovers: %v", err)
	}
}

func TestArtifactRejectsMissingAmbiguousTraversalAndSymlinkTargets(t *testing.T) {
	for _, names := range [][]string{{"markdown.md"}, {"middle_json.json", "middle.json"}, {"../middle_json.json"}, {"/middle_json.json"}, {`outside\middle_json.json`}} {
		a := &artifactStore{path: filepath.Join(t.TempDir(), "artifacts")}
		ref := artifactRef{DocumentID: "doc-a", SourceFileID: "f-a", ScopeIdentity: ScopeIdentity{TenantID: 7, KnowledgeBaseID: "kb-a"}, MinerUVersion: "4.0.1"}
		if err := a.persist(ref, middleZIP(t, names...)); err == nil {
			t.Fatalf("unsafe archive accepted: %v", names)
		}
		if _, err := os.Stat(a.path); !os.IsNotExist(err) {
			t.Fatal("invalid ZIP caused artifact publication")
		}
	}
	root, outside := t.TempDir(), t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "doc-a")); err != nil {
		t.Skip("symlink permission unavailable")
	}
	a := &artifactStore{path: root}
	ref := artifactRef{DocumentID: "doc-a", SourceFileID: "f-a", ScopeIdentity: ScopeIdentity{TenantID: 7, KnowledgeBaseID: "kb-a"}, MinerUVersion: "4.0.1"}
	if err := a.persist(ref, middleZIP(t, "middle_json.json")); err == nil {
		t.Fatal("symlink document accepted")
	}
	if entries, _ := os.ReadDir(outside); len(entries) != 0 {
		t.Fatal("artifact escaped root")
	}
}

func TestProxyRequiresTrustedDocumentBindingBeforeArtifactJob(t *testing.T) {
	f := fakeMinerUFixture(t)
	root := filepath.Join(t.TempDir(), "data", "parser_artifacts")
	p, base := proxyFixture(t, f.server.URL, testBackendKey, WithArtifactRoot(root))
	ep, key := scopeEndpoint(base, "a"), testBindings()[0].APIKey
	file := uploadThroughProxy(t, ep, key, testPDF())
	if r := proxyCall(t, "POST", ep+"/parse/jobs", key, jobPayload(file), ""); r.status != 409 || f.jobs.Load() != 0 {
		t.Fatalf("missing native doc id: HTTP %d jobs=%d", r.status, f.jobs.Load())
	}
	if err := p.BindDocument("b", file, "doc-a"); err == nil {
		t.Fatal("bound another scope's input")
	}
	if err := p.BindDocument("a", file, "../escape"); err == nil {
		t.Fatal("unsafe document id bound")
	}
	if err := p.BindDocument("a", file, "doc-a"); err != nil {
		t.Fatal(err)
	}
	if err := p.BindDocument("a", file, "doc-b"); err == nil {
		t.Fatal("silently changed doc ownership")
	}
	r := proxyCall(t, "POST", ep+"/parse/jobs", key, jobPayload(file), "")
	if r.status != 202 {
		t.Fatalf("bound job HTTP %d", r.status)
	}
	job := r.object(t)["job_id"].(string)
	// A backend must not make an early ZIP bypass the completed-job archival gate.
	s := p.scopes["a"]
	s.mu.Lock()
	s.files["early-zip"] = false
	s.mu.Unlock()
	if early := proxyCall(t, "GET", ep+"/files/early-zip/content", key, nil, ""); early.status != 409 {
		t.Fatalf("early ZIP bypassed archive gate: %d", early.status)
	}
	proxyCall(t, "GET", ep+"/parse/jobs/"+job, key, nil, "")
	z := proxyCall(t, "GET", ep+"/files/z-"+job+"/content", key, nil, "")
	if z.status != 200 {
		t.Fatalf("artifact download HTTP %d", z.status)
	}
	if _, err := os.Stat(filepath.Join(root, "doc-a", "middle.json")); err != nil {
		t.Fatal(err)
	}
}
