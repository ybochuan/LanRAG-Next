package business

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"
)

// Staging is authoritative for governance after approval, including withdrawal.
const feedbackRecords = `WITH records AS (
 SELECT q.feedback_id,COALESCE(s.status,q.status) AS status,COALESCE(s.revision,q.revision) AS revision,
  q.submitted_at,'qa' AS source,q.rating,COALESCE(s.risk,'') AS risk,s.ready_at,
  CASE WHEN ? THEN json_object('text',q.text,'reason',q.reason,'feedback_context',json(q.submitted_context),
   'citation_chunks',json(q.citation_chunks),'feedback_anchor',json(q.feedback_anchor),'applies_when',q.applies_when,'reviewed_guidance',q.reviewed_guidance) END AS detail,
  '' AS legacy,COALESCE(s.status,q.status)='needs_evidence' OR (q.status='pending_review' AND json_array_length(q.citation_chunks)=0) AS evidence_required
 FROM ext_qa_feedback q LEFT JOIN ext_feedback_staging s USING(tenant_id,knowledge_base_id,feedback_id)
 WHERE q.tenant_id=? AND q.knowledge_base_id=?
 UNION ALL
 SELECT s.feedback_id,s.status,s.revision,e.occurred_at,CASE WHEN b.feedback_id IS NULL THEN 'manual' ELSE 'qa' END,
  '',s.risk,s.ready_at,NULL,json_object('text',s.text,'topic',s.topic,'sample_decision',coalesce(r.decision,''),'sample_selected_at',coalesce(r.selected_at,'')),0
 FROM ext_feedback_staging s JOIN ext_feedback_events e ON e.tenant_id=s.tenant_id AND e.knowledge_base_id=s.knowledge_base_id AND e.event_id=s.feedback_id
 LEFT JOIN ext_feedback_qa_bindings b ON b.tenant_id=s.tenant_id AND b.knowledge_base_id=s.knowledge_base_id AND b.feedback_id=s.feedback_id
 LEFT JOIN ext_feedback_sample_reviews r ON r.tenant_id=s.tenant_id AND r.knowledge_base_id=s.knowledge_base_id AND r.feedback_id=s.feedback_id
 WHERE s.tenant_id=? AND s.knowledge_base_id=? AND s.knowledge_base_id<>'' AND NOT EXISTS (
  SELECT 1 FROM ext_qa_feedback q WHERE q.tenant_id=s.tenant_id AND q.knowledge_base_id=s.knowledge_base_id AND q.feedback_id=s.feedback_id
 )
)`

func (s *Service) listFeedback(ctx context.Context, c Command) ([]map[string]any, error) {
	if s == nil || s.db == nil {
		return nil, ErrUnavailable
	}
	inbox := c.Action == "qa_feedback_list"
	if c.Scope.TenantID == 0 || c.Payload.DocumentID != "" || c.ID != "" && !validID(c.ID) ||
		(inbox && (c.Scope.KnowledgeBaseID != "" || !c.IncludeFeedbackContext)) || (!inbox && !validID(c.Scope.KnowledgeBaseID)) {
		return nil, ErrInvalid
	}
	if c.Page == 0 {
		c.Page = 1
	}
	if c.PageSize == 0 {
		c.PageSize = 20
	}
	maxPage, maxSize := 10000, 20
	if inbox {
		maxPage, maxSize = 1000000, 100
	}
	if c.Page < 1 || c.Page > maxPage || c.PageSize < 1 || c.PageSize > maxSize {
		return nil, ErrInvalid
	}
	switch c.ListStatus {
	case "", "received", "pending_review", "needs_evidence", "record_only", "approved", "active", "rejected", "quarantined", "withdrawn":
	default:
		return nil, ErrInvalid
	}
	out := []map[string]any{}
	detailed := c.ID != "" && c.IncludeFeedbackContext
	params := append([]any{detailed}, args(c.Scope)...)
	params = append(params, args(c.Scope, c.ListStatus, c.ListStatus, c.ID, c.ID, c.PageSize, (c.Page-1)*c.PageSize)...)
	err := s.db.View(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, feedbackRecords+` SELECT feedback_id,status,revision,submitted_at,source,rating,risk,ready_at,detail,legacy,evidence_required
   FROM records WHERE (?='' OR status=?) AND (?='' OR feedback_id=?)
   ORDER BY substr(submitted_at,1,19) DESC,
    substr(CASE WHEN substr(submitted_at,20,1)='.' THEN substr(submitted_at,21,length(submitted_at)-21) ELSE '' END || '000000000',1,9) DESC,
    feedback_id DESC LIMIT ? OFFSET ?`, params...)
		if err != nil {
			return err
		}
		defer rows.Close()
		legacyDetail := false
		for rows.Next() {
			var id, status, submitted, source, rating, risk, legacy string
			var ready, detail sql.NullString
			var revision int64
			var evidence bool
			if err = rows.Scan(&id, &status, &revision, &submitted, &source, &rating, &risk, &ready, &detail, &legacy, &evidence); err != nil {
				return err
			}
			row := map[string]any{}
			if legacy != "" {
				if err = json.Unmarshal([]byte(legacy), &row); err != nil {
					return err
				}
				if source == "qa" && !c.IncludeFeedbackContext {
					delete(row, "text")
					delete(row, "topic")
				}
				legacyDetail = source == "qa" && detailed
				row["ready_at"] = nil
			} else {
				if detail.Valid {
					if err = json.Unmarshal([]byte(detail.String), &row); err != nil {
						return err
					}
					if ctx, ok := row["feedback_context"].(map[string]any); ok && ctx["snapshot_restricted"] == true {
						delete(row, "text")
						delete(row, "citation_chunks")
					}
					if row["feedback_anchor"] == nil {
						delete(row, "feedback_anchor")
						delete(row, "applies_when")
						delete(row, "reviewed_guidance")
					}
				}
				row["rating"], row["evidence_required"] = rating, evidence
			}
			if risk != "" {
				row["risk"] = risk
			}
			if ready.Valid {
				row["ready_at"] = ready.String
			}
			row["id"], row["status"], row["revision"] = id, status, revision
			row["source"], row["submitted_at"] = source, submitted
			out = append(out, row)
		}
		if err = rows.Err(); err != nil {
			return err
		}
		if err = rows.Close(); err != nil {
			return err
		}
		if legacyDetail {
			return feedbackReviewContext(ctx, tx, c, out[0])
		}
		if detailed && len(out) == 1 && out[0]["feedback_anchor"] != nil {
			checks, err := annotationChecks(ctx, tx, c)
			if err != nil {
				return err
			}
			out[0]["annotation_checks"] = checks
		}
		return nil
	})
	return out, err
}

type annotationCheck struct {
	Outcome   string `json:"outcome"`
	CheckedAt time.Time `json:"checked_at"`
}

func annotationChecks(ctx context.Context, tx *sql.Tx, c Command) ([]annotationCheck, error) {
	checks := []annotationCheck{}
	var approvedAt string
	err := tx.QueryRowContext(ctx, `SELECT occurred_at FROM ext_business_audit
 WHERE tenant_id=? AND knowledge_base_id=? AND aggregate_id=? AND action='feedback_approve' AND status='approved'
 ORDER BY sequence DESC LIMIT 1`, args(c.Scope, c.ID)...).Scan(&approvedAt)
	if err == sql.ErrNoRows {
		return checks, nil
	}
	if err != nil {
		return nil, err
	}
	approved, err := time.Parse(time.RFC3339Nano, approvedAt)
	if err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT c.checked_at, CASE
  WHEN c.delivery='unavailable' THEN 'guidance_unavailable'
  WHEN m.value='matched' AND c.delivery='applied' AND s.state='success' THEN 'sent'
  ELSE coalesce(m.value,'source_mismatch') END
 FROM ext_annotation_checks c JOIN ext_provenance_steps s USING(tenant_id,knowledge_base_id,run_id,step_id)
 LEFT JOIN json_each(c.matches_json) m ON m.key=?
 WHERE c.tenant_id=? AND c.knowledge_base_id=? AND c.checked_at>=?
 ORDER BY c.sequence DESC LIMIT 10`, append([]any{c.ID}, args(c.Scope, approved.UTC().Format("2006-01-02T15:04:05.000000000Z"))...)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var check annotationCheck
		var checkedAt string
		if err := rows.Scan(&checkedAt, &check.Outcome); err != nil {
			return nil, err
		}
		check.CheckedAt, err = time.Parse(time.RFC3339Nano, checkedAt)
		if err != nil {
			return nil, err
		}
		checks = append(checks, check)
	}
	return checks, rows.Err()
}
