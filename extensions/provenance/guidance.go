package provenance

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"

	"github.com/ybochuan/LanRAG-Next/extensions/corrections"
	"github.com/ybochuan/LanRAG-Next/extensions/guidance"
)

// GuidanceRequest binds a retry-stable call to a model step when one is available.
type GuidanceRequest struct {
	StepID              string               `json:"step_id"`
	CallID              string               `json:"call_id"`
	CorrectionEvidence  bool                 `json:"correction_evidence,omitempty"`
	Query               string               `json:"query,omitempty"`
	Question            string               `json:"question,omitempty"`
	ConsumedToolOutputs []ConsumedToolOutput `json:"consumed_tool_outputs,omitempty"`
}

// ConsumedToolOutput identifies native search output included in the current request.
type ConsumedToolOutput struct {
	StepID     string `json:"step_id"`
	ToolCallID string `json:"tool_call_id"`
	OutputHash string `json:"output_hash"`
}

// GuidanceResult returns only the issued ephemeral addition and its bound step.
type GuidanceResult struct {
	guidance.Selection
	StepID             string `json:"step_id"`
	CorrectionDisabled bool   `json:"correction_disabled,omitempty"`
	DeadlinePolicy     string `json:"deadline_policy,omitempty"`
}

const deadlineEvidencePolicy = "【期限证据规则】只回答来源明示的时长和单位。来源仅写‘活动结束后’等相对描述时，" +
	"照原意转述。未明示的起算日、周末、节假日、调休、顺延和截止日期，答‘资料未明确，无法确定具体日期’；禁止推算、举例或列计算步骤。"

// PrepareGuidance authenticates and issues instructions in one writer transaction.
// Governance instructions are recorded separately from native document evidence.
func (r *Runs) PrepareGuidance(ctx context.Context, h RunHandle, q GuidanceRequest) (out GuidanceResult, err error) {
	if !r.available() {
		return out, ErrUnavailable
	}
	if q.StepID != "" && !id(q.StepID) || !digest(q.CallID) || len(q.ConsumedToolOutputs) > 128 || !corrections.ValidQuery(q.Query) || !corrections.ValidQuery(q.Question) || q.StepID == "" && len(q.ConsumedToolOutputs) != 0 {
		return out, ErrInvalid
	}
	err = r.db.Write(ctx, func(tx *sql.Tx) error {
		run, err := r.authenticated(ctx, tx, h, false)
		if err != nil {
			return err
		}
		if !r.now().Before(run.ExpiresAt) || run.State != "open" && run.State != "unattributed" {
			return ErrConflict
		}
		if q.StepID == "" {
			if run.State != "unattributed" {
				return ErrConflict
			}
		} else {
			kind, err := stepOpen(ctx, tx, h, q.StepID)
			if err != nil || kind != "model" {
				return ErrConflict
			}
		}
		steps, err := verifiedSearchOutputSteps(ctx, tx, h, q.ConsumedToolOutputs)
		if err != nil {
			return err
		}
		scope := guidance.Scope{TenantID: h.Scope.TenantID, KnowledgeBaseID: h.Scope.KnowledgeBaseID, UserID: run.Identity.UserID, SessionID: run.Identity.SessionID, RunID: h.RunID, StepID: q.StepID, ConsumedToolSteps: steps, CorrectionEvidence: q.CorrectionEvidence, Query: q.Query, Question: q.Question, Now: r.now()}
		selection, err := guidance.SelectTx(ctx, tx, scope, q.CallID)
		if err != nil {
			return err
		}
		if q.StepID != "" {
			if err := guidance.RecordCheckTx(ctx, tx, scope, selection, r.now()); err != nil {
				return err
			}
		}
		out = GuidanceResult{Selection: selection, StepID: q.StepID}
		if q.CorrectionEvidence && q.StepID != "" && len(selection.CorrectionItems) == 0 {
			out.DeadlinePolicy = deadlineEvidencePolicy
		}
		if (selection.Content == "" && selection.AnnotationContent == "" && selection.CorrectionContent == "") || q.StepID == "" {
			return nil
		}
		var instruction any = selection.Content
		if selection.CorrectionContent != "" {
			instruction = []string{selection.Content, selection.AnnotationPolicy, selection.AnnotationContent, selection.CorrectionContent}
		} else if selection.AnnotationContent != "" {
			instruction = []string{selection.Content, selection.AnnotationPolicy, selection.AnnotationContent}
		}
		body, err := json.Marshal(instruction)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(body)
		hash := hex.EncodeToString(sum[:])
		var oldCall, oldHash string
		err = tx.QueryRowContext(ctx, "SELECT call_id,content_hash FROM ext_guidance_step_inputs WHERE tenant_id=? AND knowledge_base_id=? AND run_id=? AND step_id=?", args(h.Scope, h.RunID, q.StepID)...).Scan(&oldCall, &oldHash)
		if err == nil {
			if oldCall != q.CallID || oldHash != hash {
				return ErrConflict
			}
			return nil
		}
		if err != sql.ErrNoRows {
			return err
		}
		_, err = tx.ExecContext(ctx, "INSERT INTO ext_guidance_step_inputs VALUES(?,?,?,?,?,?)", args(h.Scope, h.RunID, q.StepID, q.CallID, hash)...)
		return err
	})
	return
}

// verifiedSearchOutputSteps only exposes successful same-run steps that committed
// the search tool call ID and output digest. It creates no new lineage facts.
func verifiedSearchOutputSteps(ctx context.Context, tx *sql.Tx, h RunHandle, outputs []ConsumedToolOutput) ([]string, error) {
	var steps []string
	counts := make(map[string]int, len(outputs))
	for _, output := range outputs {
		counts[output.ToolCallID]++
	}
	for _, output := range outputs {
		if !id(output.StepID) || !id(output.ToolCallID) || !digest(output.OutputHash) || counts[output.ToolCallID] != 1 {
			continue
		}
		body, _ := json.Marshal([]string{"agent-search-output-v1", output.ToolCallID, output.OutputHash})
		sum := sha256.Sum256(body)
		var ok bool
		err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM ext_provenance_steps
 WHERE tenant_id=? AND knowledge_base_id=? AND run_id=? AND step_id=? AND kind='tool' AND state='success' AND output_hash=?)`,
			args(h.Scope, h.RunID, output.StepID, hex.EncodeToString(sum[:]))...).Scan(&ok)
		if err != nil {
			return nil, err
		}
		if ok {
			steps = append(steps, output.StepID)
		}
	}
	return steps, nil
}
