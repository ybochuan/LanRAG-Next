package business

import (
	"context"
	"database/sql"
	"encoding/json"
	"strconv"
	"strings"
)

func bindQAFeedback(ctx context.Context, tx *sql.Tx, c Command, feedbackID string) error {
	if !validID(c.Payload.SessionID) || !validID(c.Payload.MessageID) {
		return ErrInvalid
	}
	tenant, owner, ok := strings.Cut(c.ActorID, ":")
	if !ok || tenant != strconv.FormatUint(c.Scope.TenantID, 10) || !validID(owner) {
		return ErrConflict
	}
	var runID, state string
	var matches int
	err := tx.QueryRowContext(ctx, `SELECT count(*),coalesce(max(run_id),''),coalesce(max(state),'') FROM ext_provenance_runs
WHERE tenant_id=? AND knowledge_base_id=? AND json_extract(payload_json,'$.identity.user_id')=?
  AND json_extract(payload_json,'$.identity.session_id')=? AND json_extract(payload_json,'$.identity.attempt_id')=?`, args(c.Scope, owner, c.Payload.SessionID, c.Payload.MessageID)...).Scan(&matches, &runID, &state)
	if err != nil {
		return err
	}
	if matches != 1 || state != "sealed" {
		return ErrConflict
	}
	var existing int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM ext_feedback_qa_bindings WHERE tenant_id=? AND knowledge_base_id=? AND run_id=?`, args(c.Scope, runID)...).Scan(&existing); err != nil {
		return err
	}
	if existing != 0 {
		return ErrConflict
	}
	rows, err := tx.QueryContext(ctx, `SELECT DISTINCT i.retrieved_chunk_id FROM ext_provenance_inputs i
JOIN ext_provenance_steps s USING (tenant_id,knowledge_base_id,run_id,step_id)
WHERE i.tenant_id=? AND i.knowledge_base_id=? AND i.run_id=? AND s.kind IN ('model','tool') AND s.state='success'
  AND i.retrieved_chunk_id<>'' ORDER BY i.retrieved_chunk_id`, args(c.Scope, runID)...)
	if err != nil {
		return err
	}
	defer rows.Close()
	var chunks []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return err
		}
		chunks = append(chunks, id)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(chunks) == 0 {
		return ErrConflict
	}
	ids, err := json.Marshal(chunks)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO ext_feedback_qa_bindings(tenant_id,knowledge_base_id,feedback_id,run_id,retrieved_chunk_ids) VALUES(?,?,?,?,?)`, args(c.Scope, feedbackID, runID, string(ids))...)
	return err
}
