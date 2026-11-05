package business

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/ybochuan/LanRAG-Next/extensions/provenance"
)

func TestQAFeedbackBindsOnlySealedVerifiedRetrieval(t *testing.T) {
	s, db, _ := fixture(t)
	ctx := context.Background()
	scope := provenance.Scope{TenantID: 7, KnowledgeBaseID: "kb"}
	if err := db.Write(ctx, func(tx *sql.Tx) error {
		for _, run := range []struct{ id, state, kind string }{{"answer", "sealed", "model"}, {"running", "open", "model"}, {"agent-search", "sealed", "tool"}, {"agent-read", "sealed", "tool"}, {"agent-reranked", "unattributed", "tool"}} {
			identity, _ := json.Marshal(provenance.RunSnapshot{Identity: provenance.RunIdentity{Scope: scope, UserID: "owner", SessionID: "session", AttemptID: run.id}})
			if _, err := tx.Exec(`INSERT INTO ext_provenance_runs VALUES('7','kb',?,1,?,?,?)`, run.id, run.state, strings.Repeat("a", 64), string(identity)); err != nil {
				return err
			}
			if _, err := tx.Exec(`INSERT INTO ext_provenance_steps(tenant_id,knowledge_base_id,run_id,step_id,kind,state) VALUES('7','kb',?,'step',?,'success')`, run.id, run.kind); err != nil {
				return err
			}
			for i, chunk := range []string{"chunk-b", "chunk-a", "chunk-a", ""} {
				if run.id == "agent-read" {
					chunk = ""
				}
				if _, err := tx.Exec(`INSERT INTO ext_provenance_inputs(tenant_id,knowledge_base_id,run_id,step_id,operation_id,evidence_hash,refs_json,retrieved_chunk_id) VALUES('7','kb',?,'step',?,?,'[]',?)`, run.id, i, strings.Repeat("b", 64), chunk); err != nil {
					return err
				}
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	c := Command{Scope: scope, ActorID: "7:owner", Action: "feedback_capture", OperationID: "capture", Payload: Payload{SessionID: "session", MessageID: "answer", Topic: "review", Text: "Check the effective date.", Negative: true}}
	for _, v := range []struct{ actor, message string }{{"7:other", "answer"}, {"8:owner", "answer"}, {"7:owner", "running"}, {"7:owner", "agent-read"}, {"7:owner", "agent-reranked"}} {
		wrong := c
		wrong.ActorID, wrong.Payload.MessageID = v.actor, v.message
		if _, err := s.Execute(ctx, wrong); !errors.Is(err, ErrConflict) {
			t.Fatalf("incorrect binding accepted: %+v (%v)", v, err)
		}
	}
	result, err := s.Execute(ctx, c)
	if err != nil || result.Status != "pending_review" {
		t.Fatal(result, err)
	}
	if again, err := s.Execute(ctx, c); err != nil || again != result {
		t.Fatal("same operation did not replay original receipt", again, err)
	}
	c.OperationID = "second-click"
	if _, err := s.Execute(ctx, c); !errors.Is(err, ErrConflict) {
		t.Fatal("another operation created a second vote for the answer", err)
	}
	c.OperationID, c.Payload.MessageID = "capture-agent", "agent-search"
	agentResult, err := s.Execute(ctx, c)
	if err != nil || agentResult.Status != "pending_review" {
		t.Fatal(agentResult, err)
	}
	if err := db.View(ctx, func(tx *sql.Tx) error {
		for _, feedback := range []Result{result, agentResult} {
			var chunks []byte
			if err := tx.QueryRow(`SELECT retrieved_chunk_ids FROM ext_feedback_qa_bindings WHERE feedback_id=?`, feedback.ID).Scan(&chunks); err != nil {
				return err
			}
			var ids []string
			if err := json.Unmarshal(chunks, &ids); err != nil || !reflect.DeepEqual(ids, []string{"chunk-a", "chunk-b"}) {
				t.Fatalf("unexpected retrieved IDs: %v (%v)", ids, err)
			}
		}
		var events int
		if err := tx.QueryRow(`SELECT count(*) FROM ext_feedback_events`).Scan(&events); err != nil || events != 2 {
			t.Fatalf("partial or duplicate event: %d (%v)", events, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
