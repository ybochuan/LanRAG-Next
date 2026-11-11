// Package guidance selects active instructions and accounts for durable session budgets.
package guidance

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
)

const prefix = "【回答指导】\n"

// qaSourceMatch leaves legacy manual/seed guidance alone. Answer-derived
// guidance requires a verified citation to the same chunk and source version.
const qaSourceMatch = ` AND (NOT EXISTS (
 SELECT 1 FROM ext_feedback_qa_bindings b
 WHERE b.tenant_id=g.tenant_id AND b.knowledge_base_id=g.knowledge_base_id AND b.feedback_id=g.feedback_id
) OR EXISTS (
 SELECT 1 FROM ext_feedback_qa_bindings b
 JOIN ext_provenance_inputs previous USING(tenant_id,knowledge_base_id,run_id)
 JOIN ext_provenance_steps done ON done.tenant_id=previous.tenant_id AND done.knowledge_base_id=previous.knowledge_base_id
  AND done.run_id=previous.run_id AND done.step_id=previous.step_id AND done.state='success'
 JOIN ext_provenance_inputs current ON current.tenant_id=b.tenant_id AND current.knowledge_base_id=b.knowledge_base_id
  AND current.run_id=? AND current.step_id=? AND current.retrieved_chunk_id=previous.retrieved_chunk_id
 JOIN json_each(previous.refs_json) oldref
 JOIN json_each(current.refs_json) ref ON json_extract(ref.value,'$.document_id')=json_extract(oldref.value,'$.document_id')
  AND json_extract(ref.value,'$.version_id')=json_extract(oldref.value,'$.version_id')
 WHERE b.tenant_id=g.tenant_id AND b.knowledge_base_id=g.knowledge_base_id AND b.feedback_id=g.feedback_id
 AND previous.retrieved_chunk_id<>''
))`

// Scope is supplied only from an authenticated native run.
type Scope struct {
	TenantID        uint64
	KnowledgeBaseID string
	UserID          string
	SessionID       string
	RunID           string
	StepID          string
}

// Item pins an issued instruction to its governance revision.
type Item struct {
	ID       string `json:"id"`
	Revision int64  `json:"revision"`
}

// Selection is an ephemeral addition, separate from the cached prompt prefix.
type Selection struct {
	CallID  string `json:"call_id"`
	Content string `json:"content"`
	Items   []Item `json:"items"`
	Units   int    `json:"units"`
}

func scopeArgs(s Scope) []any {
	return []any{strconv.FormatUint(s.TenantID, 10), s.KnowledgeBaseID, s.RunID}
}

// SelectTx uses the caller's single-writer transaction, including retries and empty selections.
// Units conservatively charge UTF-8 bytes plus 16 message-framing tokens.
func SelectTx(ctx context.Context, tx *sql.Tx, s Scope, callID string) (Selection, error) {
	out := Selection{CallID: callID, Items: []Item{}}
	key := append(scopeArgs(s), callID)
	var raw []byte
	err := tx.QueryRowContext(ctx, "SELECT selection_json FROM ext_guidance_calls WHERE tenant_id=? AND knowledge_base_id=? AND run_id=? AND call_id=?", key...).Scan(&raw)
	if err == nil {
		if json.Unmarshal(raw, &out) != nil {
			return Selection{}, errors.New("invalid guidance receipt")
		}
		current, err := currentTx(ctx, tx, s, out)
		if err != nil {
			return Selection{}, err
		}
		if !current {
			return Selection{CallID: callID, Items: []Item{}}, nil
		}
		return out, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return out, err
	}
	tenant := strconv.FormatUint(s.TenantID, 10)
	var spent int
	if err := tx.QueryRowContext(ctx, "SELECT coalesce(sum(units),0) FROM ext_guidance_calls WHERE tenant_id=? AND user_id=? AND session_id=?", tenant, s.UserID, s.SessionID).Scan(&spent); err != nil {
		return out, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT g.guidance_id,g.revision,g.text FROM ext_feedback_guidances g
WHERE g.tenant_id=? AND g.knowledge_base_id=? AND g.status='active'`+qaSourceMatch+`
AND (SELECT count(*) FROM ext_guidance_call_items i JOIN ext_guidance_calls c USING(tenant_id,knowledge_base_id,run_id,call_id)
 WHERE i.tenant_id=g.tenant_id AND i.knowledge_base_id=g.knowledge_base_id AND i.guidance_id=g.guidance_id AND c.user_id=? AND c.session_id=?)<2
ORDER BY g.confidence DESC,g.created_at,g.guidance_id`, tenant, s.KnowledgeBaseID, s.RunID, s.StepID, s.UserID, s.SessionID)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var item Item
		var text string
		if err := rows.Scan(&item.ID, &item.Revision, &text); err != nil {
			rows.Close()
			return out, err
		}
		content := out.Content
		if content == "" {
			content = prefix
		}
		content += "- " + text + "\n"
		units := len(content) + 16
		if text == "" || spent+units > 800 {
			continue
		}
		out.Content = content
		out.Units = units
		out.Items = append(out.Items, item)
		if len(out.Items) == 3 {
			break
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	raw, err = json.Marshal(out)
	if err != nil {
		return out, err
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO ext_guidance_calls VALUES(?,?,?,?,?,?,?,?)", append(key, s.UserID, s.SessionID, out.Units, string(raw))...); err != nil {
		return out, err
	}
	for _, item := range out.Items {
		if _, err := tx.ExecContext(ctx, "INSERT INTO ext_guidance_call_items VALUES(?,?,?,?,?,?)", append(key, item.ID, item.Revision)...); err != nil {
			return out, err
		}
	}
	return out, nil
}

func currentTx(ctx context.Context, tx *sql.Tx, s Scope, selection Selection) (bool, error) {
	content := ""
	for _, item := range selection.Items {
		var text string
		err := tx.QueryRowContext(ctx, "SELECT g.text FROM ext_feedback_guidances g WHERE g.tenant_id=? AND g.knowledge_base_id=? AND g.guidance_id=? AND g.revision=? AND g.status='active'"+qaSourceMatch,
			strconv.FormatUint(s.TenantID, 10), s.KnowledgeBaseID, item.ID, item.Revision, s.RunID, s.StepID).Scan(&text)
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if content == "" {
			content = prefix
		}
		content += "- " + text + "\n"
	}
	return content == selection.Content, nil
}
