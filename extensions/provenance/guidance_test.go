package provenance

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
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

func TestGuidanceUsesOnlyConsumedAgentSearchOutput(t *testing.T) {
	r, h := runFixture(t)
	ctx := t.Context()
	contentHash := strings.Repeat("d", 64)
	if err := r.db.Write(ctx, func(tx *sql.Tx) error {
		for _, stmt := range []string{
			"INSERT INTO ext_lineage_node VALUES('v1','7','kb','document_version','doc','now')",
			"INSERT INTO ext_document_version_family VALUES('family','7','kb','doc','','v1','v1',1,'now','now')",
			"INSERT INTO ext_document_version VALUES('v1','7','kb','family','doc','" + contentHash + "',1,'event-doc','','fixture','now')",
			`INSERT INTO ext_source_snapshot_evidence VALUES('7','kb','v1','{"chunks":[{"id":"pin","content_hash":"` + contentHash + `"}]}','` + contentHash + `')`,
			"INSERT INTO ext_feedback_events VALUES('7','kb','review','manual','review','',0,'now')",
			"INSERT INTO ext_feedback_staging VALUES('7','kb','review','review','Use five working days','medium','approved',2,'now')",
			`INSERT INTO ext_qa_feedback(tenant_id,knowledge_base_id,feedback_id,submitted_at,rating,reason,text,submitted_context,citation_chunks,feedback_anchor,status,revision) VALUES('7','kb','review','now','correction','','','{}','[]','{"document_id":"doc","version_id":"v1","chunk_id":"pin","content_hash":"` + contentHash + `"}','approved',2)`,
			"INSERT INTO ext_feedback_guidances VALUES('7','kb','guide','review','feedback','review','Use five working days',0.75,'active',1,'now')",
		} {
			if _, err := tx.Exec(stmt); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	begin := func(run RunHandle, op, kind string) string {
		t.Helper()
		s, err := r.BeginStep(ctx, run, StepRequest{OperationID: op, Kind: kind})
		if err != nil {
			t.Fatal(err)
		}
		return s.StepID
	}
	beginSearch := func(run RunHandle, op, chunk string, complete, verified bool) ConsumedToolOutput {
		t.Helper()
		output := ConsumedToolOutput{StepID: begin(run, "tool-"+op, "tool"), ToolCallID: "call-" + op, OutputHash: strings.Repeat("c", 64)}
		if verified {
			if err := r.RecordInput(ctx, run, InputRequest{OperationID: "input-" + op, StepID: output.StepID, Evidence: InputEvidence{
				DocumentID: "doc", ChunkID: chunk, Field: "content", ValueHash: strings.Repeat("a", 64), AgentSearch: true,
			}}); err != nil {
				t.Fatal(err)
			}
		}
		if complete {
			encoded, _ := json.Marshal([]string{"agent-search-output-v1", output.ToolCallID, output.OutputHash})
			digest := sha256.Sum256(encoded)
			if err := r.FinishStep(ctx, run, StepResult{OperationID: "finish-" + op, StepID: output.StepID, Outcome: "success", InputHash: strings.Repeat("b", 64), OutputHash: hex.EncodeToString(digest[:])}); err != nil {
				t.Fatal(err)
			}
		}
		return output
	}
	foreign, err := r.Begin(ctx, RunIdentity{Scope: testScope, UserID: "user", SessionID: "session", AttemptID: "other-attempt", AuthorizedDocumentIDs: []string{"doc"}}, "begin-foreign")
	if err != nil {
		t.Fatal(err)
	}
	pin := beginSearch(h, "pin", "pin", true, true)
	unrelated := beginSearch(h, "unrelated", "same-document-unrelated", true, true)
	unverified := beginSearch(h, "unverified", "pin", true, false)
	unfinished := beginSearch(h, "unfinished", "pin", false, true)
	foreignOutput := beginSearch(foreign, "foreign", "pin", true, true)
	wrongCall := pin
	wrongCall.ToolCallID = "call-other"
	wrongHash := pin
	wrongHash.OutputHash = strings.Repeat("e", 64)
	for _, test := range []struct {
		name    string
		outputs []ConsumedToolOutput
		want    bool
	}{
		{name: "omitted"},
		{name: "unrelated-chunk-same-document", outputs: []ConsumedToolOutput{unrelated}},
		{name: "unverified-result", outputs: []ConsumedToolOutput{unverified}},
		{name: "unfinished-tool", outputs: []ConsumedToolOutput{unfinished}},
		{name: "foreign-run", outputs: []ConsumedToolOutput{foreignOutput}},
		{name: "wrong-tool-call", outputs: []ConsumedToolOutput{wrongCall}},
		{name: "wrong-output-digest", outputs: []ConsumedToolOutput{wrongHash}},
		{name: "consumed-pinned-chunk", outputs: []ConsumedToolOutput{pin, unrelated}, want: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			sum := sha256.Sum256([]byte(test.name))
			q := GuidanceRequest{StepID: begin(h, "model-"+test.name, "model"), CallID: hex.EncodeToString(sum[:]), ConsumedToolOutputs: test.outputs}
			out, err := r.PrepareGuidance(ctx, h, q)
			if err != nil || strings.Contains(out.AnnotationContent, "Use five working days") != test.want || out.Content != "" {
				t.Fatalf("selection=%+v err=%v", out, err)
			}
			if test.want {
				q.ConsumedToolOutputs = nil
				replay, err := r.PrepareGuidance(ctx, h, q)
				if err != nil || len(replay.Items) != 0 || replay.AnnotationContent != "" {
					t.Fatalf("selection replay without current evidence=%+v err=%v", replay, err)
				}
			}
		})
	}
}
