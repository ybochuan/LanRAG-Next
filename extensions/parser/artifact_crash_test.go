package parser

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/store"
)

func artifactRowCount(t *testing.T, db *store.DB) int {
	t.Helper()
	var count int
	if err := db.View(context.Background(), func(tx *sql.Tx) error { return tx.QueryRow("SELECT COUNT(*) FROM ext_parser_artifacts").Scan(&count) }); err != nil {
		t.Fatal(err)
	}
	return count
}

func TestArtifactBarrierFailureBlocksNativeACKAndSQL(t *testing.T) {
	for _, failure := range []string{"stage", "root"} {
		t.Run(failure, func(t *testing.T) {
			f := fakeMinerUFixture(t)
			newHost, db := persistentFixture(t, f)
			h := newHost()
			file := nativeInput(t, h, "doc-a")
			created := nativeCall(t, h, "POST", "/parser/native/v1/parse/jobs", "doc-a", jobPayload(file))
			if created.status != 202 {
				t.Fatal(created.status)
			}
			job := created.object(t)["job_id"].(string)
			if r := nativeCall(t, h, "GET", "/parser/native/v1/parse/jobs/"+job, "doc-a", nil); r.status != 200 {
				t.Fatal(r.status)
			}
			id := nativeIdentity{ScopeIdentity: ScopeIdentity{TenantID: 7, KnowledgeBaseID: "kb-a"}, DocID: "doc-a"}
			a := h.documents[id].artifacts
			a.syncDirectory = func(_ *os.Root, name string) error {
				if (failure == "stage" && strings.HasPrefix(name, ".pending-")) || (failure == "root" && name == ".") {
					return errors.New("injected directory barrier failure")
				}
				return nil
			}
			if r := nativeCall(t, h, "GET", "/parser/native/v1/files/z-"+job+"/content", "doc-a", nil); r.status != 503 {
				t.Fatalf("failed barrier acknowledged: HTTP%d", r.status)
			}
			if count := artifactRowCount(t, db); count != 0 {
				t.Fatalf("premature artifact SQL=%d", count)
			}
			a.syncDirectory = nil
			if r := nativeCall(t, h, "GET", "/parser/native/v1/files/z-"+job+"/content", "doc-a", nil); r.status != 200 {
				t.Fatalf("barrier repair failed: HTTP%d", r.status)
			}
			if artifactRowCount(t, db) != 1 || f.jobs.Load() != 1 {
				t.Fatalf("repair duplicated registration or parse jobs=%d", f.jobs.Load())
			}
		})
	}
}

// These are real process crashes. They do not simulate lost disk writes or
// claim physical power-cut behavior of Windows, DrvFS, the VM or hardware.
func TestArtifactPublicationCrashWindows(t *testing.T) {
	for _, mode := range []string{"stage_synced", "renamed", "root_synced", "sql_before_ack"} {
		t.Run(mode, func(t *testing.T) {
			f := fakeMinerUFixture(t)
			root := t.TempDir()
			path := filepath.Join(root, "state.db")
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestArtifactPublicationCrashHelper$")
			cmd.Env = append(os.Environ(), "AGENTRAG_ARTIFACT_CRASH_MODE="+mode, "AGENTRAG_ARTIFACT_CRASH_ROOT="+root, "AGENTRAG_ARTIFACT_CRASH_UPSTREAM="+f.server.URL)
			output, err := cmd.CombinedOutput()
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 23 {
				t.Fatalf("crash mode=%s err=%v output=%s", mode, err, output)
			}
			db, err := store.OpenDurable(context.Background(), path)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			expected := 0
			if mode == "sql_before_ack" {
				expected = 1
			}
			if count := artifactRowCount(t, db); count != expected {
				t.Fatalf("SQL before recovery=%d want %d", count, expected)
			}
			entries, err := os.ReadDir(filepath.Join(root, "artifacts"))
			if err != nil {
				t.Fatal(err)
			}
			var remnants []string
			for _, entry := range entries {
				if strings.HasPrefix(entry.Name(), ".pending-") {
					remnants = append(remnants, entry.Name())
				}
			}
			if mode == "stage_synced" && len(remnants) != 1 {
				t.Fatalf("missing unowned staging remnant: %v", remnants)
			}
			before, readErr := os.ReadFile(filepath.Join(root, "artifacts", "doc-a", "middle.json"))
			if mode == "stage_synced" && !os.IsNotExist(readErr) {
				t.Fatal("stage-only crash published final document")
			}
			if mode != "stage_synced" && readErr != nil {
				t.Fatal("publication window missing archive", readErr)
			}
			h, err := NewPersistentNativeHandler(crashConfig(root, f.server.URL), db)
			if err != nil {
				t.Fatal(err)
			}
			defer h.Close()
			// The fake backend uses j-1 for its one formal job. Restored output
			// capability must suffice; recovery never reuploads/recreates that job.
			if r := nativeCall(t, h, "GET", "/parser/native/v1/files/z-j-1/content", "doc-a", nil); r.status != 200 {
				t.Fatalf("recovery HTTP%d", r.status)
			}
			if artifactRowCount(t, db) != 1 || f.jobs.Load() != 1 {
				t.Fatalf("duplicate side effects after crash: jobs=%d", f.jobs.Load())
			}
			after, err := os.ReadFile(filepath.Join(root, "artifacts", "doc-a", "middle.json"))
			if err != nil {
				t.Fatal(err)
			}
			if before != nil && !bytes.Equal(before, after) {
				t.Fatal("recovery changed original artifact")
			}
			for _, name := range remnants {
				if _, err := os.Stat(filepath.Join(root, "artifacts", name, "middle.json")); err != nil {
					t.Fatal("recovery deleted old pending data", err)
				}
			}
			if err := db.View(context.Background(), func(tx *sql.Tx) error {
				var stored []byte
				if err := tx.QueryRow("SELECT middle_json FROM ext_parser_artifacts WHERE tenant_id=7 AND knowledge_base_id='kb-a' AND doc_id='doc-a'").Scan(&stored); err != nil {
					return err
				}
				if !bytes.Equal(stored, after) {
					t.Error("SQL differs from original file")
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
			if r := nativeCall(t, h, "GET", "/parser/native/v1/files/z-j-1/content", "other-doc", nil); r.status != 403 {
				t.Fatal("crash recovery leaked another document's capability")
			}
		})
	}
}

type exitBeforeArtifactACK struct{ headers http.Header }

func (w *exitBeforeArtifactACK) Header() http.Header { return w.headers }
func (*exitBeforeArtifactACK) WriteHeader(code int) {
	if code != 200 {
		os.Exit(24)
	}
	os.Exit(23)
}
func (*exitBeforeArtifactACK) Write(_ []byte) (int, error) { panic("ACK writer did not terminate") }

func TestArtifactPublicationCrashHelper(t *testing.T) {
	mode := os.Getenv("AGENTRAG_ARTIFACT_CRASH_MODE")
	if mode == "" {
		t.Skip("subprocess only")
	}
	root := os.Getenv("AGENTRAG_ARTIFACT_CRASH_ROOT")
	db, err := store.OpenDurable(context.Background(), filepath.Join(root, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	h, err := NewPersistentNativeHandler(crashConfig(root, os.Getenv("AGENTRAG_ARTIFACT_CRASH_UPSTREAM")), db)
	if err != nil {
		t.Fatal(err)
	}
	file := nativeInput(t, h, "doc-a")
	created := nativeCall(t, h, "POST", "/parser/native/v1/parse/jobs", "doc-a", jobPayload(file))
	if created.status != 202 {
		t.Fatal(created.status)
	}
	job := created.object(t)["job_id"].(string)
	if r := nativeCall(t, h, "GET", "/parser/native/v1/parse/jobs/"+job, "doc-a", nil); r.status != 200 {
		t.Fatal(r.status)
	}
	id := nativeIdentity{ScopeIdentity: ScopeIdentity{TenantID: 7, KnowledgeBaseID: "kb-a"}, DocID: "doc-a"}
	h.documents[id].artifacts.syncDirectory = func(root *os.Root, name string) error {
		if mode == "renamed" && name == "." {
			os.Exit(23)
		}
		if err := syncArtifactDirectory(root, name); err != nil {
			return err
		}
		if (mode == "stage_synced" && strings.HasPrefix(name, ".pending-")) || (mode == "root_synced" && name == ".") {
			os.Exit(23)
		}
		return nil
	}
	if mode == "sql_before_ack" {
		request, _ := http.NewRequest("GET", "http://127.0.0.1/parser/native/v1/files/z-"+job+"/content", nil)
		request.Header.Set("Authorization", "Bearer "+nativeTestKey)
		request.Header.Set("X-AgentRAG-Bridge-Version", "1")
		request.Header.Set("X-AgentRAG-Tenant-ID", "7")
		request.Header.Set("X-AgentRAG-KB-ID", "kb-a")
		request.Header.Set("X-AgentRAG-Doc-ID", "doc-a")
		h.ServeHTTP(&exitBeforeArtifactACK{headers: make(http.Header)}, request)
	} else {
		nativeCall(t, h, "GET", "/parser/native/v1/files/z-"+job+"/content", "doc-a", nil)
	}
	t.Fatal("configured crash window not reached")
}
