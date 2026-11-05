package business

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"
)

func (s *Service) feedback(ctx context.Context, c Command) (Result, error) {
	return s.transact(ctx, c, func(tx *sql.Tx) (Result, error) {
		p := c.Payload
		out := Result{ID: c.ID, Revision: c.ExpectedRevision + 1}
		switch c.Action {
		case "feedback_create", "feedback_capture", "guidance_seed":
			if strings.TrimSpace(p.Text) == "" || len(p.Text) > 16000 || strings.TrimSpace(p.Topic) == "" || len(p.Topic) > 128 || c.ExpectedRevision != 0 {
				return Result{}, ErrInvalid
			}
			out.ID = newID()
			if out.ID == "" {
				return Result{}, ErrUnavailable
			}
			out.Status = "pending_review"
			source := "manual"
			if c.Action == "guidance_seed" {
				source = "seed"
				out.Status = "active"
			}
			if _, e := tx.ExecContext(ctx, `INSERT INTO ext_feedback_events VALUES(?,?,?,?,?,?,?,?)`, args(c.Scope, out.ID, source, p.Topic, p.Text, p.Negative, s.now().UTC().Format(time.RFC3339Nano))...); e != nil {
				return Result{}, e
			}
			if c.Action == "feedback_capture" {
				if e := bindQAFeedback(ctx, tx, c, out.ID); e != nil {
					return Result{}, e
				}
			}
			if source == "seed" {
				var negatives int
				if e := tx.QueryRowContext(ctx, `SELECT count(*) FROM ext_feedback_events WHERE tenant_id=? AND knowledge_base_id=? AND topic=? AND negative=1`, args(c.Scope, p.Topic)...).Scan(&negatives); e != nil {
					return Result{}, e
				}
				if negatives >= 2 {
					out.Status = "quarantined"
				}
				_, e := tx.ExecContext(ctx, `INSERT INTO ext_feedback_guidances VALUES(?,?,?,NULL,'seed',?,?,0.75,?,1,?)`, args(c.Scope, out.ID, p.Topic, p.Text, out.Status, s.now().UTC().Format(time.RFC3339Nano))...)
				return out, e
			}
			if p.Risk == "" {
				p.Risk = "medium"
			}
			if p.Risk != "low" && p.Risk != "medium" && p.Risk != "high" {
				return Result{}, ErrInvalid
			}
			var ready any
			if p.Risk == "low" {
				out.Status = "approved"
				ready = s.now().Add(24 * time.Hour).UTC().Format(time.RFC3339Nano)
			}
			if p.Negative {
				var negatives int
				if e := tx.QueryRowContext(ctx, `SELECT count(*) FROM ext_feedback_events WHERE tenant_id=? AND knowledge_base_id=? AND topic=? AND negative=1`, args(c.Scope, p.Topic)...).Scan(&negatives); e != nil {
					return Result{}, e
				}
				if negatives >= 2 {
					if _, e := tx.ExecContext(ctx, `UPDATE ext_feedback_guidances SET status='quarantined',revision=revision+1 WHERE tenant_id=? AND knowledge_base_id=? AND topic=? AND status='active'`, args(c.Scope, p.Topic)...); e != nil {
						return Result{}, e
					}
				}
			}
			_, e := tx.ExecContext(ctx, `INSERT INTO ext_feedback_staging VALUES(?,?,?,?,?,?,?,1,?)`, args(c.Scope, out.ID, p.Topic, p.Text, p.Risk, out.Status, ready)...)
			return out, e
		case "guidance_quarantine_topic":
			if p.Topic == "" {
				return Result{}, ErrInvalid
			}
			var negatives int
			if e := tx.QueryRowContext(ctx, `SELECT count(*) FROM ext_feedback_events WHERE tenant_id=? AND knowledge_base_id=? AND topic=? AND negative=1`, args(c.Scope, p.Topic)...).Scan(&negatives); e != nil {
				return Result{}, e
			}
			if negatives < 2 {
				return Result{}, ErrConflict
			}
			_, e := tx.ExecContext(ctx, `UPDATE ext_feedback_guidances SET status='quarantined',revision=revision+1 WHERE tenant_id=? AND knowledge_base_id=? AND topic=? AND status='active'`, args(c.Scope, p.Topic)...)
			out.ID = p.Topic
			out.Status = "quarantined"
			return out, e
		}
		var status, risk, topic, text string
		var rev int64
		var ready sql.NullString
		if e := tx.QueryRowContext(ctx, `SELECT status,risk,topic,text,revision,ready_at FROM ext_feedback_staging WHERE tenant_id=? AND knowledge_base_id=? AND feedback_id=?`, args(c.Scope, c.ID)...).Scan(&status, &risk, &topic, &text, &rev, &ready); e != nil {
			return Result{}, ErrConflict
		}
		if rev != c.ExpectedRevision {
			return Result{}, ErrConflict
		}
		switch c.Action {
		case "feedback_approve":
			if status != "pending_review" {
				return Result{}, ErrConflict
			}
			hours := 24
			if risk == "medium" {
				hours = 48
			} else if risk == "high" {
				hours = 72
			}
			ready = sql.NullString{String: s.now().Add(time.Duration(hours) * time.Hour).UTC().Format(time.RFC3339Nano), Valid: true}
			out.Status = "approved"
		case "feedback_activate":
			var negatives int
			if e := tx.QueryRowContext(ctx, `SELECT count(*) FROM ext_feedback_events WHERE tenant_id=? AND knowledge_base_id=? AND topic=? AND negative=1`, args(c.Scope, topic)...).Scan(&negatives); e != nil {
				return Result{}, e
			}
			if negatives >= 2 {
				return Result{}, ErrConflict
			}
			if status != "approved" || !ready.Valid {
				return Result{}, ErrConflict
			}
			at, e := time.Parse(time.RFC3339Nano, ready.String)
			if e != nil || s.now().Before(at) || risk == "high" && !p.SecondConfirmation {
				return Result{}, ErrConflict
			}
			out.Status = "active"
			if _, e = tx.ExecContext(ctx, `INSERT INTO ext_feedback_guidances VALUES(?,?,?,?,'feedback',?,?,0.75,'active',1,?)`, args(c.Scope, c.ID, c.ID, topic, text, s.now().UTC().Format(time.RFC3339Nano))...); e != nil {
				return Result{}, e
			}
		case "feedback_reject":
			if status != "pending_review" {
				return Result{}, ErrConflict
			}
			out.Status = "rejected"
		case "feedback_quarantine":
			if status != "approved" && status != "active" && status != "pending_review" {
				return Result{}, ErrConflict
			}
			out.Status = "quarantined"
		case "feedback_withdraw":
			if status != "approved" && status != "active" {
				return Result{}, ErrConflict
			}
			out.Status = "withdrawn"
		default:
			return Result{}, ErrInvalid
		}
		if out.Status == "quarantined" || out.Status == "withdrawn" {
			if _, e := tx.ExecContext(ctx, `UPDATE ext_feedback_guidances SET status=?,revision=revision+1 WHERE tenant_id=? AND knowledge_base_id=? AND guidance_id=?`, append([]any{out.Status}, args(c.Scope, c.ID)...)...); e != nil {
				return Result{}, e
			}
		}
		_, e := tx.ExecContext(ctx, `UPDATE ext_feedback_staging SET status=?,revision=?,ready_at=? WHERE tenant_id=? AND knowledge_base_id=? AND feedback_id=?`, append([]any{out.Status, out.Revision, ready}, args(c.Scope, c.ID)...)...)
		return out, e
	})
}

// List returns inspectable, scoped management records; it does not inject them into QA.
func (s *Service) List(ctx context.Context, scopeScope Command) ([]map[string]any, error) {
	c := scopeScope
	table, idcol := "ext_feedback_staging", "feedback_id"
	fields := `json_object('text',text,'topic',topic,'risk',risk,'ready_at',ready_at,'source',CASE WHEN EXISTS (
SELECT 1 FROM ext_feedback_qa_bindings b WHERE b.tenant_id=ext_feedback_staging.tenant_id AND b.knowledge_base_id=ext_feedback_staging.knowledge_base_id AND b.feedback_id=ext_feedback_staging.feedback_id
) THEN 'qa' ELSE 'manual' END)`
	switch c.Action {
	case "relation_list":
		table, idcol = "ext_document_relations", "relation_id"
		fields = `json_object('document_id',document_id,'target_document_id',target_document_id,'edge_type',edge_type,'confidence',confidence,'source_version_id',source_version_id,'target_version_id',target_version_id)`
	case "feedback_list":
	case "guidance_list":
		table, idcol = "ext_feedback_guidances", "guidance_id"
		fields = `json_object('text',text,'topic',topic,'source',source,'confidence',confidence)`
	case "revision_list":
		table, idcol = "ext_revision_proposals", "proposal_id"
		fields = `json_object('proposed_patch',proposed_patch,'source_document_id',source_document_id,'source_version_id',source_version_id,'family_id',family_id,'guidance_ids',json(trigger_guidance_ids),'publication_key',publication_key,'target_document_id',target_document_id,'publish_error',publish_error)`
	default:
		return nil, ErrInvalid
	}
	out := []map[string]any{}
	if s == nil || s.db == nil {
		return nil, ErrUnavailable
	}
	if c.Page == 0 {
		c.Page = 1
	}
	if c.PageSize == 0 {
		c.PageSize = 20
	}
	if c.Page < 1 || c.Page > 10000 || c.PageSize < 1 || c.PageSize > 20 {
		return nil, ErrInvalid
	}
	e := s.db.View(ctx, func(tx *sql.Tx) error {
		where := "tenant_id=? AND knowledge_base_id=?"
		params := args(c.Scope)
		if c.ID != "" {
			if !validID(c.ID) {
				return ErrInvalid
			}
			where += " AND " + idcol + "=?"
			params = append(params, c.ID)
		}
		params = append(params, c.PageSize, (c.Page-1)*c.PageSize)
		rows, e := tx.QueryContext(ctx, `SELECT `+idcol+`,status,revision,`+fields+` FROM `+table+` WHERE `+where+` ORDER BY `+idcol+` LIMIT ? OFFSET ?`, params...)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var id, status string
			var rev int64
			var details []byte
			if e = rows.Scan(&id, &status, &rev, &details); e != nil {
				return e
			}
			var row map[string]any
			if e = json.Unmarshal(details, &row); e != nil {
				return e
			}
			row["id"] = id
			row["status"] = status
			row["revision"] = rev
			out = append(out, row)
		}
		return rows.Err()
	})
	return out, e
}
