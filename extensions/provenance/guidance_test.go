package provenance

import (
	"context"
	"database/sql"
	"strings"
	"testing"
)

func TestGuidanceRequiresActualModelStepAndRollsBackFailedBinding(t *testing.T) {
	r, h := runFixture(t)
	ctx := context.Background()
	if err := r.db.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.Exec("INSERT INTO ext_feedback_guidances VALUES('7','kb','guide',NULL,'seed','topic','Use current clauses',0.75,'active',1,'now')")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	model, err := r.BeginStep(ctx, h, StepRequest{OperationID: "model", Kind: "model"})
	if err != nil {
		t.Fatal(err)
	}
	q := GuidanceRequest{StepID: model.StepID, CallID: strings.Repeat("a", 64)}
	bad := h
	bad.Capability = strings.Repeat("b", 64)
	if _, err = r.PrepareGuidance(ctx, bad, q); err == nil {
		t.Fatal("foreign capability admitted")
	}
	if err := r.db.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.Exec("CREATE TRIGGER ext_test_guidance_failure BEFORE INSERT ON ext_guidance_step_inputs BEGIN SELECT RAISE(ABORT,'injected'); END")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err = r.PrepareGuidance(ctx, h, q); err == nil {
		t.Fatal("binding failure acknowledged")
	}
	if err := r.db.Write(ctx, func(tx *sql.Tx) error {
		var calls int
		if err := tx.QueryRow("SELECT count(*) FROM ext_guidance_calls").Scan(&calls); err != nil {
			return err
		}
		if calls != 0 {
			t.Fatal("failed binding consumed budget")
		}
		_, err := tx.Exec("DROP TRIGGER ext_test_guidance_failure")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	out, err := r.PrepareGuidance(ctx, h, q)
	if err != nil || len(out.Items) != 1 || out.Content == "" {
		t.Fatalf("selection=%+v err=%v", out, err)
	}
	replay, err := r.PrepareGuidance(ctx, h, q)
	if err != nil || out.Content != replay.Content {
		t.Fatal("idempotent selection failed", err)
	}
	tool, err := r.BeginStep(ctx, h, StepRequest{OperationID: "tool", Kind: "tool"})
	if err != nil {
		t.Fatal(err)
	}
	q.StepID = tool.StepID
	if _, err = r.PrepareGuidance(ctx, h, q); err == nil {
		t.Fatal("tool can request generation instructions")
	}
	if err := r.FinishStep(ctx, h, StepResult{OperationID: "unknown", StepID: tool.StepID, Outcome: "unknown"}); err != nil {
		t.Fatal(err)
	}
	q.StepID = ""
	out, err = r.PrepareGuidance(ctx, h, q)
	if err != nil || out.Content == "" {
		t.Fatal("unattributed authorized run lost guidance", err)
	}
	run, err := r.Inspect(ctx, h)
	if err != nil || run.State != "unattributed" {
		t.Fatal("guidance changed attribution", run.State, err)
	}
}
