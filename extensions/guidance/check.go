package guidance

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"
)

// RecordCheckTx snapshots source matching and lifecycle decisions in the selection transaction.
// Only matches need individual outcomes. The absence of an exact match is represented by its absence from the map.
func RecordCheckTx(ctx context.Context, tx *sql.Tx, s Scope, selection Selection, now time.Time) error {
	tenant := scopeArgs(s)[0]
	steps, _ := json.Marshal(s.ConsumedToolSteps)
	items, err := json.Marshal(selection.Items)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO ext_annotation_checks(tenant_id,knowledge_base_id,run_id,step_id,call_id,checked_at,matches_json)
 SELECT ?,?,?,?,?,?, (
  SELECT json_group_object(g.feedback_id, CASE g.status
   WHEN 'approved' THEN CASE WHEN julianday(g.ready_at)>julianday(?) THEN 'cooling_down' ELSE 'awaiting_activation' END
   WHEN 'active' THEN CASE WHEN EXISTS (SELECT 1 FROM json_each(?) item WHERE json_extract(item.value,'$.id')=g.feedback_id)
      THEN 'matched' ELSE 'budget_or_limit' END
   ELSE g.status END)
  FROM ext_feedback_staging g JOIN ext_qa_feedback q USING(tenant_id,knowledge_base_id,feedback_id)
  WHERE g.tenant_id=? AND g.knowledge_base_id=? AND q.feedback_anchor IS NOT NULL
  `+anchoredSourceMatch+`
 )
 WHERE EXISTS (SELECT 1 FROM ext_qa_feedback q WHERE q.tenant_id=? AND q.knowledge_base_id=? AND q.feedback_anchor IS NOT NULL)
 ON CONFLICT(tenant_id,knowledge_base_id,run_id,step_id) DO NOTHING`, append(scopeArgs(s), s.StepID, selection.CallID, checkTime(now), checkTime(now), string(items), tenant, s.KnowledgeBaseID,
		s.RunID, s.StepID, string(steps), tenant, s.KnowledgeBaseID)...)
	if err != nil {
		return err
	}
	return pruneChecks(ctx, tx, s)
}

// FinishCheckTx records whether native code attached an annotation or could not obtain a guidance response.
// A missing prepare on a recovered connection is observable only when the terminal receipt is acknowledged.
func FinishCheckTx(ctx context.Context, tx *sql.Tx, s Scope, inputHash, delivery string, now time.Time) error {
	if delivery == "applied" {
		_, err := tx.ExecContext(ctx, `UPDATE ext_annotation_checks SET delivery='applied'
 WHERE tenant_id=? AND knowledge_base_id=? AND run_id=? AND step_id=? AND call_id<>?`, append(scopeArgs(s), s.StepID, inputHash)...)
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO ext_annotation_checks(tenant_id,knowledge_base_id,run_id,step_id,call_id,checked_at,matches_json,delivery)
 SELECT ?,?,?,?,?,?,'{}','unavailable'
 WHERE EXISTS (SELECT 1 FROM ext_qa_feedback q WHERE q.tenant_id=? AND q.knowledge_base_id=? AND q.feedback_anchor IS NOT NULL)
 ON CONFLICT(tenant_id,knowledge_base_id,run_id,step_id) DO UPDATE SET delivery='unavailable'`,
		append(scopeArgs(s), s.StepID, inputHash, checkTime(now), scopeArgs(s)[0], s.KnowledgeBaseID)...); err != nil {
		return err
	}
	return pruneChecks(ctx, tx, s)
}

func pruneChecks(ctx context.Context, tx *sql.Tx, s Scope) error {
	tenant := scopeArgs(s)[0]
	_, err := tx.ExecContext(ctx, `DELETE FROM ext_annotation_checks WHERE tenant_id=? AND knowledge_base_id=? AND sequence<(
 SELECT sequence FROM ext_annotation_checks WHERE tenant_id=? AND knowledge_base_id=? ORDER BY sequence DESC LIMIT 1 OFFSET 199
 )`, tenant, s.KnowledgeBaseID, tenant, s.KnowledgeBaseID)
	return err
}

func checkTime(now time.Time) string {
	return now.UTC().Format("2006-01-02T15:04:05.000000000Z")
}
