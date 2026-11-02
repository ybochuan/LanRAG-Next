package provenance

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// This verifier is deliberately confined to tests. Production must verify native components.
type inputFixture struct{}

type pendingInputFixture struct {
	entered chan struct{}
	release chan struct{}
}

func (f pendingInputFixture) VerifyInput(context.Context, Scope, InputEvidence) ([]SourceRef, error) {
	close(f.entered)
	<-f.release
	return nil, ErrEvidence
}

func (inputFixture) VerifyInput(_ context.Context, _ Scope, e InputEvidence) ([]SourceRef, error) {
	if e.DocumentID != "doc" || e.ValueHash != strings.Repeat("a", 64) {
		return nil, ErrEvidence
	}
	return []SourceRef{{"doc", "v1"}}, nil
}

func runFixture(t *testing.T) (*Runs, RunHandle) {
	t.Helper()
	l, a := setup(t)
	seedSource(t, l, a, "doc")
	r := NewRuns(l.db, inputFixture{}, a, []byte(strings.Repeat("k", 32)))
	h, e := r.Begin(context.Background(), RunIdentity{Scope: testScope, UserID: "user", SessionID: "session", AttemptID: "attempt", AuthorizedDocumentIDs: []string{"doc"}}, "begin")
	if e != nil {
		t.Fatal(e)
	}
	return r, h
}

func finishModel(t *testing.T, r *Runs, h RunHandle, known bool) RunSnapshot {
	t.Helper()
	ctx := context.Background()
	s, e := r.BeginStep(ctx, h, StepRequest{OperationID: "step", Kind: "model"})
	if e != nil {
		t.Fatal(e)
	}
	if known {
		e = r.RecordInput(ctx, h, InputRequest{OperationID: "input", StepID: s.StepID, Evidence: InputEvidence{DocumentID: "doc", Field: "content", ChunkID: "chunk", ValueHash: strings.Repeat("a", 64)}})
		if e != nil {
			t.Fatal(e)
		}
	}
	e = r.FinishStep(ctx, h, StepResult{OperationID: "step-done", StepID: s.StepID, InputHash: strings.Repeat("b", 64), OutputHash: strings.Repeat("c", 64), Outcome: "success"})
	if e != nil {
		t.Fatal(e)
	}
	state, e := r.Inspect(ctx, h)
	if e != nil {
		t.Fatal(e)
	}
	return state
}

func TestRunSealCASAndReplay(t *testing.T) {
	r, h := runFixture(t)
	state := finishModel(t, r, h, true)
	var wins atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, e := r.Finish(context.Background(), h, FinishRequest{OperationID: "seal-" + string(rune('a'+i)), ExpectedRevision: state.Revision})
			if e == nil {
				wins.Add(1)
			} else if !errors.Is(e, ErrConflict) {
				t.Errorf("seal: %v", e)
			}
		}(i)
	}
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatalf("winners=%d", wins.Load())
	}
	sealed, e := r.Inspect(context.Background(), h)
	if e != nil || sealed.State != "sealed" || len(sealed.Sources) != 1 {
		t.Fatalf("%+v %v", sealed, e)
	}
	h2, e := r.Begin(context.Background(), sealed.Identity, "begin")
	if e != nil || h != h2 {
		t.Fatalf("begin replay identity changed: %v", e)
	}
	_, e = r.Begin(context.Background(), RunIdentity{Scope: testScope, UserID: "other", SessionID: "session", AttemptID: "attempt", AuthorizedDocumentIDs: []string{"doc"}}, "begin")
	if !errors.Is(e, ErrConflict) {
		t.Fatalf("rebound begin=%v", e)
	}
}

func TestRunCannotSealUnknownPendingEmptyOrWrongIdentity(t *testing.T) {
	for _, mode := range []string{"unknown", "pending", "empty", "capability", "scope", "abort"} {
		t.Run(mode, func(t *testing.T) {
			r, h := runFixture(t)
			state := finishModel(t, r, h, mode != "empty")
			switch mode {
			case "unknown":
				s, e := r.BeginStep(context.Background(), h, StepRequest{OperationID: "opaque", Kind: "tool"})
				if e != nil {
					t.Fatal(e)
				}
				if e = r.FinishStep(context.Background(), h, StepResult{OperationID: "opaque-done", StepID: s.StepID, Outcome: "unknown"}); e != nil {
					t.Fatal(e)
				}
			case "pending":
				if _, e := r.BeginStep(context.Background(), h, StepRequest{OperationID: "pending", Kind: "tool"}); e != nil {
					t.Fatal(e)
				}
			case "capability":
				h.Capability = strings.Repeat("f", 64)
			case "scope":
				h.Scope.TenantID++
			case "abort":
				if _, e := r.Finish(context.Background(), h, FinishRequest{OperationID: "abort", ExpectedRevision: state.Revision, Abort: true}); e != nil {
					t.Fatal(e)
				}
			}
			if s, e := r.Inspect(context.Background(), h); e == nil {
				state = s
			}
			if _, e := r.Finish(context.Background(), h, FinishRequest{OperationID: "seal", ExpectedRevision: state.Revision}); e == nil {
				t.Fatal("unsafe seal succeeded")
			}
		})
	}
}

func TestProducerBindsActualStepOwnerBytesAndSealedRun(t *testing.T) {
	r, h := runFixture(t)
	finishModel(t, r, h, true)
	ctx := context.Background()
	step, e := r.BeginStep(ctx, h, StepRequest{OperationID: "write", Kind: "write"})
	if e != nil {
		t.Fatal(e)
	}
	p, e := r.ReserveProducer(ctx, h, ProducerRequest{OperationID: "reserve", StepID: step.StepID, CopyID: "copy", Path: "reports/result.md", Binding: testBinding})
	if e != nil {
		t.Fatal(e)
	}
	p2, e := r.ReserveProducer(ctx, h, ProducerRequest{OperationID: "reserve", StepID: step.StepID, CopyID: "copy", Path: "reports/result.md", Binding: testBinding})
	if e != nil || p != p2 {
		t.Fatalf("reservation retry: %+v %v", p2, e)
	}
	if _, e = r.SealedCopy(ctx, testScope, "copy", testBinding, p.ProducerID); e == nil {
		t.Fatal("unsealed producer admitted")
	}
	if e = r.CommitProducer(ctx, h, ProducerResult{OperationID: "commit", ProducerID: p.ProducerID, StepID: step.StepID, Binding: testBinding}); e != nil {
		t.Fatal(e)
	}
	if e = r.CommitProducer(ctx, h, ProducerResult{OperationID: "commit", ProducerID: p.ProducerID, StepID: step.StepID, Binding: testBinding}); e != nil {
		t.Fatal(e)
	}
	if e = r.FinishStep(ctx, h, StepResult{OperationID: "write-done", StepID: step.StepID, Outcome: "success", InputHash: strings.Repeat("b", 64), OutputHash: testBinding.SHA256}); e != nil {
		t.Fatal(e)
	}
	state, e := r.Inspect(ctx, h)
	if e != nil {
		t.Fatal(e)
	}
	sealed, e := r.Finish(ctx, h, FinishRequest{OperationID: "seal", ExpectedRevision: state.Revision})
	if e != nil {
		t.Fatal(e)
	}
	if replay, e := r.Finish(ctx, h, FinishRequest{OperationID: "seal", ExpectedRevision: state.Revision}); e != nil || replay.Revision != sealed.Revision {
		t.Fatalf("seal retry: %v", e)
	}
	proof, e := r.SealedCopy(ctx, testScope, "copy", testBinding, p.ProducerID)
	if e != nil || len(proof.Sources) != 1 || proof.RunID != h.RunID {
		t.Fatalf("proof=%+v %v", proof, e)
	}
	for _, mode := range []string{"owner", "hash", "resource", "session", "copy", "scope"} {
		b, s, copyID := testBinding, testScope, "copy"
		switch mode {
		case "owner":
			b.UserID = "other"
		case "hash":
			b.SHA256 = strings.Repeat("e", 64)
		case "resource":
			b.ResourceID = "other"
		case "session":
			b.SessionID = "other"
		case "copy":
			copyID = "other"
		case "scope":
			s.TenantID++
		}
		if _, e = r.SealedCopy(ctx, s, copyID, b, p.ProducerID); e == nil {
			t.Fatalf("%s rebound producer", mode)
		}
	}
}

func TestUnverifiedInputAndMismatchedWritePermanentlyTaintRun(t *testing.T) {
	for _, mode := range []string{"input", "bytes"} {
		t.Run(mode, func(t *testing.T) {
			r, h := runFixture(t)
			finishModel(t, r, h, true)
			ctx := context.Background()
			s, e := r.BeginStep(ctx, h, StepRequest{OperationID: "next", Kind: "write"})
			if e != nil {
				t.Fatal(e)
			}
			if mode == "input" {
				e = r.RecordInput(ctx, h, InputRequest{OperationID: "unverified", StepID: s.StepID, Evidence: InputEvidence{DocumentID: "doc", Field: "content", ValueHash: strings.Repeat("f", 64)}})
			} else {
				p, reserveErr := r.ReserveProducer(ctx, h, ProducerRequest{OperationID: "reserve", StepID: s.StepID, CopyID: "copy", Path: "result.md", Binding: testBinding})
				if reserveErr != nil {
					t.Fatal(reserveErr)
				}
				wrong := testBinding
				wrong.SHA256 = strings.Repeat("f", 64)
				e = r.CommitProducer(ctx, h, ProducerResult{OperationID: "mismatch", ProducerID: p.ProducerID, StepID: s.StepID, Binding: wrong})
				if retryErr := r.CommitProducer(ctx, h, ProducerResult{OperationID: "retry-correct", ProducerID: p.ProducerID, StepID: s.StepID, Binding: testBinding}); retryErr == nil {
					t.Fatal("mismatch washed away by later commit")
				}
			}
			if e == nil {
				t.Fatal("invalid evidence acknowledged")
			}
			v, e := r.Inspect(ctx, h)
			if e != nil || v.State != "unattributed" {
				t.Fatalf("state=%s error=%v", v.State, e)
			}
		})
	}
}

func TestPendingProofCannotRaceStepCompletion(t *testing.T) {
	r, h := runFixture(t)
	finishModel(t, r, h, true)
	ctx := context.Background()
	s, e := r.BeginStep(ctx, h, StepRequest{OperationID: "pending-step", Kind: "model"})
	if e != nil {
		t.Fatal(e)
	}
	f := pendingInputFixture{make(chan struct{}), make(chan struct{})}
	r.verifier = f
	result := make(chan error, 1)
	go func() {
		result <- r.RecordInput(ctx, h, InputRequest{OperationID: "pending-input", StepID: s.StepID, Evidence: InputEvidence{DocumentID: "doc", Field: "content", ValueHash: strings.Repeat("a", 64)}})
	}()
	<-f.entered
	e = r.FinishStep(ctx, h, StepResult{OperationID: "premature-finish", StepID: s.StepID, Outcome: "success", InputHash: strings.Repeat("a", 64), OutputHash: strings.Repeat("b", 64)})
	close(f.release)
	if inputErr := <-result; inputErr == nil {
		t.Fatal("invalid proof acknowledged")
	}
	if e == nil {
		t.Fatal("step completed while source proof was pending")
	}
	v, e := r.Inspect(ctx, h)
	if e != nil || v.State != "unattributed" {
		t.Fatalf("failed concurrent proof washed out: %s %v", v.State, e)
	}
}

func TestRunAuthorizationScopeIsExplicit(t *testing.T) {
	r, _ := runFixture(t)
	i := RunIdentity{Scope: testScope, UserID: "user", SessionID: "session", AttemptID: "whole", AuthorizationKind: "knowledge_base"}
	if _, e := r.Begin(context.Background(), i, "explicit-whole"); e != nil {
		t.Fatal("native whole-KB authority cannot be represented", e)
	}
	i.AuthorizationKind = ""
	if _, e := r.Begin(context.Background(), i, "implicit-whole"); e == nil {
		t.Fatal("missing scope silently expanded to whole KB")
	}
	i.AuthorizationKind = "knowledge_base"
	i.AuthorizedDocumentIDs = []string{"doc"}
	if _, e := r.Begin(context.Background(), i, "ambiguous"); e == nil {
		t.Fatal("ambiguous scope accepted")
	}
}
