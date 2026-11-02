package provenance

import (
	"context"
	"database/sql"
	"encoding/json"
)

// InputEvidence describes the actual consumed native field; it carries hashes only.
// Composite/transformed inputs need an adapter; a citation is never evidence.
type InputEvidence struct {
	DocumentID string `json:"document_id"`
	ChunkID    string `json:"chunk_id,omitempty"`
	Field      string `json:"field"`
	ValueHash  string `json:"value_hash"`
}

// InputVerifier must consult current native evidence and return immutable refs.
type InputVerifier interface {
	VerifyInput(context.Context, Scope, InputEvidence) ([]SourceRef, error)
}

// StepRequest starts one actual model/tool/write operation, independently of parallel steps.
type StepRequest struct {
	OperationID string `json:"operation_id"`
	Kind        string `json:"kind"`
}

// StepHandle identifies a step inside its capability-protected run.
type StepHandle struct {
	StepID string `json:"step_id"`
}

// InputRequest records one actually consumed field in an open step.
type InputRequest struct {
	OperationID string        `json:"operation_id"`
	StepID      string        `json:"step_id"`
	Evidence    InputEvidence `json:"evidence"`
}

// StepResult is supplied by trusted native call sites after actual completion.
// Success requires exact request/output digests; unknown/error can never seal.
type StepResult struct {
	OperationID string `json:"operation_id"`
	StepID      string `json:"step_id"`
	Outcome     string `json:"outcome"`
	InputHash   string `json:"input_hash"`
	OutputHash  string `json:"output_hash"`
}

// BeginStep persists the step before a producer/model call can be acknowledged.
func (r *Runs) BeginStep(ctx context.Context, h RunHandle, q StepRequest) (s StepHandle, err error) {
	if q.Kind != "model" && q.Kind != "tool" && q.Kind != "write" {
		return s, ErrInvalid
	}
	err = r.mutate(ctx, h, q.OperationID, "step", q, &s, func(tx *sql.Tx, v *RunSnapshot) error {
		var count int
		if e := tx.QueryRowContext(ctx, "SELECT count(*) FROM ext_provenance_steps WHERE tenant_id=? AND knowledge_base_id=? AND run_id=?", args(h.Scope, h.RunID)...).Scan(&count); e != nil {
			return e
		}
		if count >= 1024 {
			return ErrInvalid
		}
		var e error
		s.StepID, e = randomFactID()
		if e != nil {
			return e
		}
		_, e = tx.ExecContext(ctx, "INSERT INTO ext_provenance_steps(tenant_id,knowledge_base_id,run_id,step_id,kind,state) VALUES(?,?,?,?,?,'open')", args(h.Scope, h.RunID, s.StepID, q.Kind)...)
		return e
	})
	if err != nil {
		return StepHandle{}, err
	}
	return
}

func stepOpen(ctx context.Context, tx *sql.Tx, h RunHandle, step string) (string, error) {
	var kind, state string
	e := tx.QueryRowContext(ctx, "SELECT kind,state FROM ext_provenance_steps WHERE tenant_id=? AND knowledge_base_id=? AND run_id=? AND step_id=?", args(h.Scope, h.RunID, step)...).Scan(&kind, &state)
	if e != nil || state != "open" {
		return "", ErrConflict
	}
	return kind, nil
}

// RecordInput resolves evidence outside the writer, then closes the local-head race.
func (r *Runs) RecordInput(ctx context.Context, h RunHandle, q InputRequest) (err error) {
	if !r.available() {
		return ErrUnavailable
	}
	if !id(q.OperationID) || !id(q.StepID) || !id(q.Evidence.DocumentID) || !id(q.Evidence.Field) || !digest(q.Evidence.ValueHash) || q.Evidence.ChunkID != "" && !id(q.Evidence.ChunkID) {
		return ErrInvalid
	}
	v, e := r.Inspect(ctx, h)
	if e != nil {
		return e
	}
	var result struct{}
	if exists, e := r.replayMutation(ctx, h, q.OperationID, "input", q, &result); e != nil || exists {
		return e
	}
	if e = r.db.View(ctx, func(tx *sql.Tx) error { _, e := stepOpen(ctx, tx, h, q.StepID); return e }); e != nil {
		return e
	}
	intentOp := payloadHash("input-intent", []string{h.RunID, q.OperationID})
	if e = r.mutate(ctx, h, intentOp, "input-intent", q, &result, func(tx *sql.Tx, v *RunSnapshot) error {
		if _, e := stepOpen(ctx, tx, h, q.StepID); e != nil {
			return e
		}
		_, e := tx.ExecContext(ctx, "INSERT INTO ext_provenance_input_intents VALUES(?,?,?,?,?,?,'pending')", args(h.Scope, h.RunID, q.StepID, q.OperationID, payloadHash("input-intent", q))...)
		return e
	}); e != nil {
		return e
	}
	defer func() {
		if err != nil {
			r.rejectInput(ctx, h, q)
		}
	}()
	allowed := v.Identity.AuthorizationKind == "knowledge_base"
	for _, doc := range v.Identity.AuthorizedDocumentIDs {
		if doc == q.Evidence.DocumentID {
			allowed = true
		}
	}
	if !allowed {
		return ErrEvidence
	}
	refs, e := r.verifier.VerifyInput(ctx, h.Scope, q.Evidence)
	if e != nil {
		return ErrEvidence
	}
	refs, e = sourceUnion(refs)
	if e != nil || len(refs) != 1 || refs[0].DocumentID != q.Evidence.DocumentID {
		return ErrEvidence
	}
	if e = r.ensureInputSource(ctx, h.Scope, refs[0]); e != nil {
		return ErrEvidence
	}
	prepared, e := r.ledger.preparePermission(ctx, Manifest{Scope: h.Scope, Sources: refs})
	if e != nil {
		return ErrEvidence
	}
	return r.mutate(ctx, h, q.OperationID, "input", q, &result, func(tx *sql.Tx, v *RunSnapshot) error {
		if _, e := stepOpen(ctx, tx, h, q.StepID); e != nil {
			return e
		}
		var intentHash, intentState string
		if e := tx.QueryRowContext(ctx, "SELECT payload_hash,state FROM ext_provenance_input_intents WHERE tenant_id=? AND knowledge_base_id=? AND run_id=? AND step_id=? AND operation_id=?", args(h.Scope, h.RunID, q.StepID, q.OperationID)...).Scan(&intentHash, &intentState); e != nil || intentHash != payloadHash("input-intent", q) || intentState != "pending" {
			return ErrEvidence
		}
		if e := r.ledger.verifyPermissionTx(ctx, tx, h.Scope, refs, prepared); e != nil {
			return ErrEvidence
		}
		for _, ref := range refs {
			if reason, e := r.ledger.sourceReason(ctx, tx, h.Scope, ref); e != nil || reason != "" {
				return ErrEvidence
			}
		}
		var e error
		v.Sources, e = sourceUnion(v.Sources, refs)
		if e != nil {
			return e
		}
		body, _ := json.Marshal(refs)
		if _, e = tx.ExecContext(ctx, "UPDATE ext_provenance_input_intents SET state='verified' WHERE tenant_id=? AND knowledge_base_id=? AND run_id=? AND operation_id=?", args(h.Scope, h.RunID, q.OperationID)...); e != nil {
			return e
		}
		_, e = tx.ExecContext(ctx, "INSERT INTO ext_provenance_inputs VALUES(?,?,?,?,?,?,?)", args(h.Scope, h.RunID, q.StepID, q.OperationID, payloadHash("input-evidence", q.Evidence), string(body))...)
		return e
	})
}

func (r *Runs) rejectInput(ctx context.Context, h RunHandle, q InputRequest) {
	var result struct{}
	_ = r.mutate(ctx, h, payloadHash("input-rejected", []string{h.RunID, q.OperationID}), "input-rejected", q, &result, func(tx *sql.Tx, v *RunSnapshot) error {
		v.State = "unattributed"
		if _, e := tx.ExecContext(ctx, "UPDATE ext_provenance_input_intents SET state='rejected' WHERE tenant_id=? AND knowledge_base_id=? AND run_id=? AND step_id=? AND operation_id=?", args(h.Scope, h.RunID, q.StepID, q.OperationID)...); e != nil {
			return e
		}
		_, e := tx.ExecContext(ctx, "UPDATE ext_provenance_steps SET state='unknown' WHERE tenant_id=? AND knowledge_base_id=? AND run_id=? AND step_id=?", args(h.Scope, h.RunID, q.StepID)...)
		return e
	})
}

// taintStep records a failed proof, never an allow. If the DB is unavailable,
// the original operation still returns an error and its open step cannot seal.
func (r *Runs) taintStep(ctx context.Context, h RunHandle, stepID, operationID string) {
	_ = r.FinishStep(ctx, h, StepResult{OperationID: payloadHash("unverified-step", []string{operationID, stepID}), StepID: stepID, Outcome: "unknown"})
}

// ensureInputSource registers only an already verified current observation. A
// racing registration is rejected, never retried by replaying a model/file action.
func (r *Runs) ensureInputSource(ctx context.Context, s Scope, ref SourceRef) error {
	var revision int64
	var version string
	e := r.db.View(ctx, func(tx *sql.Tx) error {
		e := tx.QueryRowContext(ctx, "SELECT revision,version_id FROM ext_provenance_source_heads WHERE tenant_id=? AND knowledge_base_id=? AND document_id=?", args(s, ref.DocumentID)...).Scan(&revision, &version)
		if e == sql.ErrNoRows {
			return nil
		}
		return e
	})
	if e != nil || version == ref.VersionID {
		return e
	}
	q := SourceRequest{Scope: s, DocumentID: ref.DocumentID, EvidenceID: ref.VersionID, ExpectedRevision: revision}
	q.OperationID = payloadHash("run-input-source", q)
	_, e = r.ledger.RegisterSource(ctx, q)
	return e
}

// FinishStep is monotonic; a failed/unknown operation irreversibly taints the run.
func (r *Runs) FinishStep(ctx context.Context, h RunHandle, q StepResult) error {
	if !id(q.StepID) || q.Outcome != "success" && q.Outcome != "unknown" && q.Outcome != "error" {
		return ErrInvalid
	}
	if q.Outcome == "success" && (!digest(q.InputHash) || !digest(q.OutputHash)) {
		return ErrInvalid
	}
	var result struct{}
	return r.mutate(ctx, h, q.OperationID, "step-finish", q, &result, func(tx *sql.Tx, v *RunSnapshot) error {
		if _, e := stepOpen(ctx, tx, h, q.StepID); e != nil {
			return e
		}
		if q.Outcome == "success" {
			var pending int
			if e := tx.QueryRowContext(ctx, "SELECT count(*) FROM ext_provenance_input_intents WHERE tenant_id=? AND knowledge_base_id=? AND run_id=? AND step_id=? AND state<>'verified'", args(h.Scope, h.RunID, q.StepID)...).Scan(&pending); e != nil {
				return e
			}
			if pending != 0 {
				return ErrConflict
			}
		}
		if q.Outcome != "success" {
			v.State = "unattributed"
		}
		_, e := tx.ExecContext(ctx, "UPDATE ext_provenance_steps SET state=?,input_hash=?,output_hash=? WHERE tenant_id=? AND knowledge_base_id=? AND run_id=? AND step_id=?", append([]any{q.Outcome, q.InputHash, q.OutputHash}, args(h.Scope, h.RunID, q.StepID)...)...)
		return e
	})
}
