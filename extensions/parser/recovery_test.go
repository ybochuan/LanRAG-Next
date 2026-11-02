package parser

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/store"
)

func persistentFixture(t *testing.T, f *fakeMinerU) (func() *NativeHandler, *store.DB) {
	t.Helper()
	root := t.TempDir()
	db, err := store.OpenDurable(context.Background(), filepath.Join(root, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	cfg := NativeConfig{UpstreamURL: f.server.URL, UpstreamKey: testBackendKey, APIKey: nativeTestKey, ArtifactRoot: filepath.Join(root, "artifacts"), Scopes: []ScopeIdentity{{TenantID: 7, KnowledgeBaseID: "kb-a"}}}
	return func() *NativeHandler {
		h, err := NewPersistentNativeHandler(cfg, db)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(h.Close)
		return h
	}, db
}

func TestRecoveryCanonicalJobAcrossUploadsAndRestart(t *testing.T) {
	f := fakeMinerUFixture(t)
	f.varyFileIDs.Store(true)
	newHost, db := persistentFixture(t, f)
	h := newHost()
	file := nativeInput(t, h, "doc-a")
	created := nativeCall(t, h, "POST", "/parser/native/v1/parse/jobs", "doc-a", jobPayload(file))
	if created.status != 202 {
		t.Fatalf("create=%d", created.status)
	}
	job := created.object(t)["job_id"].(string)
	h.Close()
	h = newHost()
	// Restored job capabilities must work before a fresh upload too.
	if r := nativeCall(t, h, "GET", "/parser/native/v1/parse/jobs/"+job, "doc-a", nil); r.status != 200 {
		t.Fatalf("restore=%d", r.status)
	}
	file2 := nativeInput(t, h, "doc-a")
	if file == file2 {
		t.Fatal("fixture did not change backend file identity")
	}
	r := nativeCall(t, h, "POST", "/parser/native/v1/parse/jobs", "doc-a", jobPayload(file2))
	if r.status != 202 || r.object(t)["job_id"] != job || f.jobs.Load() != 1 {
		t.Fatalf("duplicate parse: HTTP %d jobs=%d", r.status, f.jobs.Load())
	}
	if r := nativeCall(t, h, "GET", "/parser/native/v1/files/z-"+job+"/content", "doc-a", nil); r.status != 200 {
		t.Fatalf("archive=%d body=%s", r.status, r.body)
	}
	if err := db.View(context.Background(), func(tx *sql.Tx) error {
		var count int
		if err := tx.QueryRow("SELECT COUNT(*) FROM ext_parser_artifacts WHERE tenant_id=7 AND doc_id='doc-a' AND artifact_format='middle_json' AND length(middle_json)>0").Scan(&count); err != nil {
			return err
		}
		if count != 1 {
			t.Errorf("registered artifacts=%d", count)
		}
		rows, err := tx.Query("PRAGMA foreign_key_check")
		if err != nil {
			return err
		}
		defer rows.Close()
		if rows.Next() {
			t.Error("foreign key violation")
		}
		return rows.Err()
	}); err != nil {
		t.Fatal(err)
	}
	if r := nativeCall(t, h, "GET", "/parser/native/v1/parse/jobs/"+job, "doc-b", nil); r.status != 403 {
		t.Fatal("restored job crossed document")
	}
}

func TestRecoveryUnknownOutcomeNeverResubmits(t *testing.T) {
	f := fakeMinerUFixture(t)
	f.failJob.Store(true)
	f.varyFileIDs.Store(true)
	newHost, _ := persistentFixture(t, f)
	h := newHost()
	file := nativeInput(t, h, "doc-a")
	if r := nativeCall(t, h, "POST", "/parser/native/v1/parse/jobs", "doc-a", jobPayload(file)); r.status != 503 {
		t.Fatal(r.status)
	}
	h.Close()
	h = newHost()
	f.failJob.Store(false)
	file = nativeInput(t, h, "doc-a")
	if r := nativeCall(t, h, "POST", "/parser/native/v1/parse/jobs", "doc-a", jobPayload(file)); r.status != 503 || f.jobs.Load() != 1 {
		t.Fatalf("unsafe replay: status=%d jobs=%d", r.status, f.jobs.Load())
	}
}

func TestRecoveryCapacityAndConcurrentSingleParse(t *testing.T) {
	f := fakeMinerUFixture(t)
	newHost, _ := persistentFixture(t, f)
	h := newHost()
	file := nativeInput(t, h, "doc-a")
	var wg sync.WaitGroup
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if r := nativeCall(t, h, "POST", "/parser/native/v1/parse/jobs", "doc-a", jobPayload(file)); r.status != 202 {
				t.Errorf("concurrent create=%d", r.status)
			}
		}()
	}
	wg.Wait()
	if f.jobs.Load() != 1 {
		t.Fatalf("concurrent duplicate jobs=%d", f.jobs.Load())
	}
	for i := 0; i < 130; i++ {
		doc := fmt.Sprintf("doc-%d", i)
		file := nativeInput(t, h, doc)
		created := nativeCall(t, h, "POST", "/parser/native/v1/parse/jobs", doc, jobPayload(file))
		if created.status != 202 {
			t.Fatalf("doc %d create=%d", i, created.status)
		}
		job := created.object(t)["job_id"].(string)
		if r := nativeCall(t, h, "GET", "/parser/native/v1/parse/jobs/"+job, doc, nil); r.status != 200 {
			t.Fatal(r.status)
		}
		if r := nativeCall(t, h, "GET", "/parser/native/v1/files/z-"+job+"/content", doc, nil); r.status != 200 {
			t.Fatalf("doc %d archive=%d", i, r.status)
		}
	}
	if r := nativeCall(t, h, "POST", "/parser/native/v1/parse/jobs", "doc-a", jobPayload(file)); r.status != 202 || f.jobs.Load() != 131 {
		t.Fatalf("eviction lost receipt: status=%d jobs=%d", r.status, f.jobs.Load())
	}
}

func TestRecoveryArtifactRegistrationFailureRepairsPublishedFiles(t *testing.T) {
	f := fakeMinerUFixture(t)
	newHost, db := persistentFixture(t, f)
	h := newHost()
	file := nativeInput(t, h, "doc-a")
	r := nativeCall(t, h, "POST", "/parser/native/v1/parse/jobs", "doc-a", jobPayload(file))
	job := r.object(t)["job_id"].(string)
	nativeCall(t, h, "GET", "/parser/native/v1/parse/jobs/"+job, "doc-a", nil)
	if err := db.Write(context.Background(), func(tx *sql.Tx) error {
		_, err := tx.Exec("CREATE TRIGGER ext_test_reject_artifact BEFORE INSERT ON ext_parser_artifacts BEGIN SELECT RAISE(ABORT,'injected'); END")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if r := nativeCall(t, h, "GET", "/parser/native/v1/files/z-"+job+"/content", "doc-a", nil); r.status != 503 {
		t.Fatal("unregistered ZIP acknowledged")
	}
	if _, err := os.Stat(filepath.Join(h.cfg.ArtifactRoot, "doc-a", "middle.json")); err != nil {
		t.Fatal("publication window not reached", err)
	}
	h.Close()
	h = newHost()
	if err := db.Write(context.Background(), func(tx *sql.Tx) error { _, err := tx.Exec("DROP TRIGGER ext_test_reject_artifact"); return err }); err != nil {
		t.Fatal(err)
	}
	if r := nativeCall(t, h, "GET", "/parser/native/v1/files/z-"+job+"/content", "doc-a", nil); r.status != 200 || f.jobs.Load() != 1 {
		t.Fatalf("repair failed %d jobs=%d", r.status, f.jobs.Load())
	}
}

// Abrupt exits bypass all defers; this tests actual SQLite recovery rather than
// reopening a gracefully closed connection or simulating an error return.
func TestRecoveryAbruptProcessExit(t *testing.T) {
	for _, mode := range []string{"intent", "receipt", "artifact"} {
		t.Run(mode, func(t *testing.T) {
			f := fakeMinerUFixture(t)
			root := t.TempDir()
			path := filepath.Join(root, "state.db")
			cmd := exec.Command(os.Args[0], "-test.run=^TestParserCrashHelper$")
			cmd.Env = append(os.Environ(), "AGENTRAG_PARSER_CRASH_MODE="+mode, "AGENTRAG_PARSER_CRASH_DB="+path, "AGENTRAG_PARSER_CRASH_ROOT="+root, "AGENTRAG_PARSER_CRASH_UPSTREAM="+f.server.URL)
			out, err := cmd.CombinedOutput()
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 23 {
				t.Fatalf("helper=%v %s", err, out)
			}
			db, err := store.OpenDurable(context.Background(), path)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			h, err := NewPersistentNativeHandler(crashConfig(root, f.server.URL), db)
			if err != nil {
				t.Fatal(err)
			}
			defer h.Close()
			file := nativeInput(t, h, "doc-a")
			r := nativeCall(t, h, "POST", "/parser/native/v1/parse/jobs", "doc-a", jobPayload(file))
			if mode == "intent" {
				if r.status != 503 || f.jobs.Load() != 0 {
					t.Fatalf("unconfirmed intent replayed: %d jobs=%d", r.status, f.jobs.Load())
				}
				return
			}
			if r.status != 202 || f.jobs.Load() != 1 {
				t.Fatalf("lost committed receipt: %d jobs=%d", r.status, f.jobs.Load())
			}
			job := r.object(t)["job_id"].(string)
			nativeCall(t, h, "GET", "/parser/native/v1/parse/jobs/"+job, "doc-a", nil)
			if r := nativeCall(t, h, "GET", "/parser/native/v1/files/z-"+job+"/content", "doc-a", nil); r.status != 200 {
				t.Fatal(r.status)
			}
		})
	}
}

func crashConfig(root, upstream string) NativeConfig {
	return NativeConfig{UpstreamURL: upstream, UpstreamKey: testBackendKey, APIKey: nativeTestKey, ArtifactRoot: filepath.Join(root, "artifacts"), Scopes: []ScopeIdentity{{TenantID: 7, KnowledgeBaseID: "kb-a"}}}
}

func TestParserCrashHelper(t *testing.T) {
	mode := os.Getenv("AGENTRAG_PARSER_CRASH_MODE")
	if mode == "" {
		t.Skip("subprocess only")
	}
	db, err := store.OpenDurable(context.Background(), os.Getenv("AGENTRAG_PARSER_CRASH_DB"))
	if err != nil {
		t.Fatal(err)
	}
	h, err := NewPersistentNativeHandler(crashConfig(os.Getenv("AGENTRAG_PARSER_CRASH_ROOT"), os.Getenv("AGENTRAG_PARSER_CRASH_UPSTREAM")), db)
	if err != nil {
		t.Fatal(err)
	}
	file := nativeInput(t, h, "doc-a")
	if mode == "intent" {
		s := h.documents[nativeIdentity{ScopeIdentity: ScopeIdentity{TenantID: 7, KnowledgeBaseID: "kb-a"}, DocID: "doc-a"}]
		// Freeze immediately after the same durable intent operation used by the
		// request path, before the remote network side effect.
		var in jobRequest
		b, _ := json.Marshal(jobPayload(file))
		_ = json.Unmarshal(b, &in)
		in.Tier = "flash"
		in.OCRMode = "txt"
		in.Files[0].Source.FileID = s.scopes["native"].hashes[file]
		b, _ = json.Marshal([]any{1, s.upstream, s.scopes["native"].identity, "doc-a", in})
		key := sha256.Sum256(b)
		s.scopes["native"].requests[key] = &jobReceipt{busy: true}
		if err := s.journal.claim(context.Background(), s.scopes["native"], key); err != nil {
			t.Fatal(err)
		}
	} else {
		r := nativeCall(t, h, "POST", "/parser/native/v1/parse/jobs", "doc-a", jobPayload(file))
		if r.status != 202 {
			t.Fatal(r.status)
		}
		if mode == "artifact" {
			job := r.object(t)["job_id"].(string)
			nativeCall(t, h, "GET", "/parser/native/v1/parse/jobs/"+job, "doc-a", nil)
			if r := nativeCall(t, h, "GET", "/parser/native/v1/files/z-"+job+"/content", "doc-a", nil); r.status != 200 {
				t.Fatal(r.status)
			}
		}
	}
	os.Exit(23)
}

func TestRecoveryClosedDBBlocksBeforeFormalParse(t *testing.T) {
	f := fakeMinerUFixture(t)
	newHost, db := persistentFixture(t, f)
	h := newHost()
	file := nativeInput(t, h, "doc-a")
	_ = db.Close()
	if r := nativeCall(t, h, "POST", "/parser/native/v1/parse/jobs", "doc-a", jobPayload(file)); r.status != 503 || f.jobs.Load() != 0 {
		t.Fatalf("uncommitted intent sent: status=%d jobs=%d", r.status, f.jobs.Load())
	}
}

func TestRecoveryDoesNotEvictActiveFormalJob(t *testing.T) {
	f := fakeMinerUFixture(t)
	f.jobGate = make(chan struct{})
	var release sync.Once
	defer release.Do(func() { close(f.jobGate) })
	newHost, _ := persistentFixture(t, f)
	h := newHost()
	file := nativeInput(t, h, "active-doc")
	done := make(chan proxyReply, 1)
	go func() {
		done <- nativeCall(t, h, "POST", "/parser/native/v1/parse/jobs", "active-doc", jobPayload(file))
	}()
	deadline := time.Now().Add(3 * time.Second)
	for f.jobs.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if f.jobs.Load() != 1 {
		t.Fatal("formal job did not start")
	}
	id := nativeIdentity{ScopeIdentity: ScopeIdentity{TenantID: 7, KnowledgeBaseID: "kb-a"}, DocID: "active-doc"}
	h.mu.Lock()
	original := h.documents[id]
	h.mu.Unlock()
	for i := 0; i < 130; i++ {
		if r := nativeCall(t, h, "GET", "/parser/native/v1/health", fmt.Sprintf("idle-%d", i), nil); r.status != 200 {
			t.Fatal(r.status)
		}
	}
	h.mu.Lock()
	preserved := h.documents[id] == original
	h.mu.Unlock()
	if !preserved {
		t.Error("active document was evicted")
	}
	release.Do(func() { close(f.jobGate) })
	if r := <-done; r.status != 202 {
		t.Fatalf("active job lost: %d", r.status)
	}
}

func TestRecoveryRefusesPreJournalArchiveWithoutReparsing(t *testing.T) {
	f := fakeMinerUFixture(t)
	newHost, _ := persistentFixture(t, f)
	cfg := newHost().cfg
	old, err := NewNativeHandler(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer old.Close()
	file := nativeInput(t, old, "legacy-doc")
	r := nativeCall(t, old, "POST", "/parser/native/v1/parse/jobs", "legacy-doc", jobPayload(file))
	job := r.object(t)["job_id"].(string)
	nativeCall(t, old, "GET", "/parser/native/v1/parse/jobs/"+job, "legacy-doc", nil)
	if r := nativeCall(t, old, "GET", "/parser/native/v1/files/z-"+job+"/content", "legacy-doc", nil); r.status != 200 {
		t.Fatal(r.status)
	}
	h := newHost()
	if r := nativeCall(t, h, "GET", "/parser/native/v1/health", "legacy-doc", nil); r.status != 503 {
		t.Fatalf("pre-journal archive silently adopted: %d", r.status)
	}
	if f.jobs.Load() != 1 {
		t.Fatal("legacy document reparsed")
	}
	if r := nativeCall(t, h, "GET", "/parser/native/v1/health", "new-doc", nil); r.status != 200 {
		t.Fatal("legacy guard blocked fresh unrelated document")
	}
}

func TestRecoveryCanceledPollStillAllowsDetachedKnownJobCancel(t *testing.T) {
	f := fakeMinerUFixture(t)
	newHost, _ := persistentFixture(t, f)
	h := newHost()
	file := nativeInput(t, h, "doc-a")
	r := nativeCall(t, h, "POST", "/parser/native/v1/parse/jobs", "doc-a", jobPayload(file))
	job := r.object(t)["job_id"].(string)
	f.cancelPoll.Store(true)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	request := httptest.NewRequest("GET", "/parser/native/v1/parse/jobs/"+job, nil).WithContext(ctx)
	request.Header.Set("Authorization", "Bearer "+nativeTestKey)
	request.Header.Set("X-AgentRAG-Bridge-Version", "1")
	request.Header.Set("X-AgentRAG-Tenant-ID", "7")
	request.Header.Set("X-AgentRAG-KB-ID", "kb-a")
	request.Header.Set("X-AgentRAG-Doc-ID", "doc-a")
	done := make(chan struct{})
	go func() { h.ServeHTTP(httptest.NewRecorder(), request); close(done) }()
	deadline := time.Now().Add(3 * time.Second)
	for f.polls.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if f.polls.Load() != 1 {
		t.Fatal("poll did not reach backend")
	}
	cancel()
	<-done
	if r := nativeCall(t, h, "DELETE", "/parser/native/v1/parse/jobs/"+job, "doc-a", nil); r.status != 200 || f.cancels.Load() != 1 {
		t.Fatalf("detached native cancellation blocked: HTTP %d cancels=%d", r.status, f.cancels.Load())
	}
}

func TestRecoveryIntentUniquenessAcrossSQLiteWriterConnections(t *testing.T) {
	f := fakeMinerUFixture(t)
	newHost, db := persistentFixture(t, f)
	h := newHost()
	_ = nativeInput(t, h, "doc-a")
	db2, err := store.OpenDurable(context.Background(), filepath.Join(filepath.Dir(h.cfg.ArtifactRoot), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()
	id := nativeIdentity{ScopeIdentity: ScopeIdentity{TenantID: 7, KnowledgeBaseID: "kb-a"}, DocID: "doc-a"}
	p := h.documents[id]
	s := p.scopes["native"]
	key := sha256.Sum256([]byte("cross-connection-claim-fixture"))
	s.mu.Lock()
	s.requests[key] = &jobReceipt{busy: true}
	s.mu.Unlock()
	js := []*parserJournal{newJournal(db, id, h.cfg), newJournal(db2, id, h.cfg)}
	results := make(chan error, 12)
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(j *parserJournal) { defer wg.Done(); results <- j.claim(context.Background(), s, key) }(js[i%2])
	}
	wg.Wait()
	close(results)
	claimed := 0
	for err := range results {
		if err == nil {
			claimed++
		} else if !errors.Is(err, errIntentExists) {
			t.Error(err)
		}
	}
	if claimed != 1 || f.jobs.Load() != 0 {
		t.Fatalf("claims=%d remote_posts=%d", claimed, f.jobs.Load())
	}
}
