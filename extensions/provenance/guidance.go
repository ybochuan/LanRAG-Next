package provenance

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"

	"github.com/ybochuan/LanRAG-Next/extensions/guidance"
)

// GuidanceRequest binds a retry-stable call to a model step when one is available.
type GuidanceRequest struct {
	StepID string `json:"step_id"`
	CallID string `json:"call_id"`
}

// GuidanceResult returns only the issued ephemeral addition and its bound step.
type GuidanceResult struct {
	guidance.Selection
	StepID string `json:"step_id"`
}

// PrepareGuidance authenticates and issues instructions in one writer transaction.
// Governance instructions are recorded separately from native document evidence.
func (r *Runs) PrepareGuidance(ctx context.Context, h RunHandle, q GuidanceRequest) (out GuidanceResult, err error) {
	if !r.available() {
		return out, ErrUnavailable
	}
	if q.StepID != "" && !id(q.StepID) || !digest(q.CallID) {
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
		scope := guidance.Scope{TenantID: h.Scope.TenantID, KnowledgeBaseID: h.Scope.KnowledgeBaseID, UserID: run.Identity.UserID, SessionID: run.Identity.SessionID, RunID: h.RunID}
		selection, err := guidance.SelectTx(ctx, tx, scope, q.CallID)
		if err != nil {
			return err
		}
		out = GuidanceResult{Selection: selection, StepID: q.StepID}
		if selection.Content == "" || q.StepID == "" {
			return nil
		}
		body, err := json.Marshal(selection.Content)
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
