package provenance

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"

	"github.com/ybochuan/LanRAG-Next/extensions/store"
)

// TestRunAuthorizationCapacityLimit pins the authorization ceiling: a run may
// carry at most 256 authorized documents, and an empty set is not a whole-KB
// grant. Both are refused before any row is written.
func TestRunAuthorizationCapacityLimit(t *testing.T) {
	ctx := context.Background()
	l, _ := setup(t)
	r := NewRuns(l.db, inputFixture{}, &testAuthority{}, []byte(strings.Repeat("k", 32)))
	runCount := func() int {
		var n int
		if err := l.db.View(ctx, func(tx *sql.Tx) error { return tx.QueryRow("SELECT count(*) FROM ext_provenance_runs").Scan(&n) }); err != nil {
			t.Fatal(err)
		}
		return n
	}
	base := RunIdentity{Scope: testScope, UserID: "user", SessionID: "session", AttemptID: "attempt"}
	tooMany := base
	for i := 0; i < 257; i++ {
		tooMany.AuthorizedDocumentIDs = append(tooMany.AuthorizedDocumentIDs, "doc-"+strconv.Itoa(i))
	}
	before := runCount()
	if _, err := r.Begin(ctx, tooMany, "too-many"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("257 documents accepted: %v", err)
	}
	if _, err := r.Begin(ctx, base, "no-documents"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("empty authorization accepted: %v", err)
	}
	if runCount() != before {
		t.Fatal("refused authorization still wrote a run row")
	}
	within := base
	for i := 0; i < 256; i++ {
		within.AuthorizedDocumentIDs = append(within.AuthorizedDocumentIDs, "doc-"+strconv.Itoa(i))
	}
	if _, err := r.Begin(ctx, within, "within"); err != nil {
		t.Fatalf("256 documents refused: %v", err)
	}
}

// TestProducerCapacityAndPathRules pins the reservation guards: at most 16
// parents, and a path the Host can canonicalise.
func TestProducerCapacityAndPathRules(t *testing.T) {
	ctx := context.Background()
	r, h := runFixture(t)
	finishModel(t, r, h, true)
	step, err := r.BeginStep(ctx, h, StepRequest{OperationID: "write", Kind: "write"})
	if err != nil {
		t.Fatal(err)
	}
	manyParents := make([]ParentRef, 0, 17)
	for i := 0; i < 17; i++ {
		manyParents = append(manyParents, ParentRef{CopyID: "parent-" + strconv.Itoa(i), Revision: 1})
	}
	if _, err := r.ReserveProducer(ctx, h, ProducerRequest{OperationID: "many", StepID: step.StepID, CopyID: "copy", Path: "reports/a.md", Binding: testBinding, Parents: manyParents}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("17 parents accepted: %v", err)
	}
	for _, path := range []string{"/etc/passwd", "../escape.md", "reports/../../escape.md", "reports\result.md", "reports/result:stream.md", strings.Repeat("a", 1025) + ".md"} {
		if _, err := r.ReserveProducer(ctx, h, ProducerRequest{OperationID: "path-" + strconv.Itoa(len(path)), StepID: step.StepID, CopyID: "copy", Path: path, Binding: testBinding}); !errors.Is(err, ErrInvalid) {
			t.Fatalf("path %q accepted: %v", path, err)
		}
	}
	if _, err := r.ReserveProducer(ctx, h, ProducerRequest{OperationID: "ok", StepID: step.StepID, CopyID: "copy", Path: "reports/result.md", Binding: testBinding}); err != nil {
		t.Fatalf("canonical path refused: %v", err)
	}
}

// sealChildAuthority rebuilds the fixture authority a child process needs to
// re-verify the sealed source set. The values mirror seedSource exactly.
func sealChildAuthority() *testAuthority {
	a := &testAuthority{sources: map[string]SourceProof{}, copies: map[string]CopyProof{}}
	a.sources["doc"] = SourceProof{Scope: testScope, DocumentID: "doc", VersionID: "v1", ContentHash: strings.Repeat("b", 64), ObservationSequence: 1, LifecycleEventID: "event-doc", EvidenceID: "doc", EvidenceHash: strings.Repeat("c", 64)}
	return a
}

// TestSealCASAcrossProcesses proves the run-head compare-and-swap holds across
// separate OS processes, not just separate transactions in one process: a
// child seals first, and the parent seal built on the older revision must lose.
func TestSealCASAcrossProcesses(t *testing.T) {
	ctx := context.Background()
	if os.Getenv("AGENTRAG_SEAL_CHILD") == "1" {
		db, err := store.OpenDurable(ctx, os.Getenv("AGENTRAG_SEAL_DB"))
		if err != nil {
			t.Fatal(err)
		}
		r := NewRuns(db, inputFixture{}, sealChildAuthority(), []byte(strings.Repeat("k", 32)))
		h := RunHandle{Scope: testScope, RunID: os.Getenv("AGENTRAG_SEAL_RUN"), Capability: os.Getenv("AGENTRAG_SEAL_CAPABILITY")}
		revision, err := strconv.ParseInt(os.Getenv("AGENTRAG_SEAL_REVISION"), 10, 64)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := r.Finish(ctx, h, FinishRequest{OperationID: "child-seal", ExpectedRevision: revision}); err != nil {
			os.Exit(3)
		}
		os.Exit(0)
	}
	r, h := runFixture(t)
	finishModel(t, r, h, true)
	state, err := r.Inspect(ctx, h)
	if err != nil {
		t.Fatal(err)
	}
	dbPath := ""
	if err := r.db.View(ctx, func(tx *sql.Tx) error {
		return tx.QueryRow("PRAGMA database_list").Scan(new(int), new(string), &dbPath)
	}); err != nil || dbPath == "" {
		t.Fatalf("cannot resolve the database path: %v", err)
	}
	if err := r.db.Close(); err != nil {
		t.Fatal(err)
	}
	child := exec.Command(os.Args[0], "-test.run=^TestSealCASAcrossProcesses$")
	child.Env = append(os.Environ(),
		"AGENTRAG_SEAL_CHILD=1",
		"AGENTRAG_SEAL_DB="+dbPath,
		"AGENTRAG_SEAL_RUN="+h.RunID,
		"AGENTRAG_SEAL_CAPABILITY="+h.Capability,
		"AGENTRAG_SEAL_REVISION="+strconv.FormatInt(state.Revision, 10))
	if out, err := child.CombinedOutput(); err != nil {
		t.Fatalf("child seal failed: %v output=%s", err, out)
	}
	db, err := store.OpenDurable(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	parent := NewRuns(db, inputFixture{}, sealChildAuthority(), []byte(strings.Repeat("k", 32)))
	sealed, err := parent.Inspect(ctx, h)
	if err != nil {
		t.Fatal(err)
	}
	if sealed.State != "sealed" || sealed.Revision <= state.Revision {
		t.Fatalf("child seal not observed: state=%s revision=%d (was %d)", sealed.State, sealed.Revision, state.Revision)
	}
	if _, err := parent.Finish(ctx, h, FinishRequest{OperationID: "parent-seal", ExpectedRevision: state.Revision}); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale cross-process seal did not conflict: %v", err)
	}
}
