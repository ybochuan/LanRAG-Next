package business

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"
)

func (s *Service) feedback(ctx context.Context, c Command) (Result, error) {
	if (c.Action == "feedback_approve" || c.Action == "feedback_activate") && s != nil && s.db != nil {
		if !validID(c.ID) || !validID(c.ActorID) || !validID(c.OperationID) || !validID(c.Scope.KnowledgeBaseID) || c.Scope.TenantID == 0 || c.ExpectedRevision < 0 {
			return Result{}, ErrInvalid
		}
		if result, found, err := s.replay(ctx, c); found || err != nil {
			return result, err
		}
		if err := s.prepareQAFeedbackAction(ctx, c); err != nil {
			return s.replayAfterObservationError(ctx, c, err)
		}
	}
	return s.transact(ctx, c, func(tx *sql.Tx) (Result, error) {
		p := c.Payload
		out := Result{ID: c.ID, Revision: c.ExpectedRevision + 1}
		switch c.Action {
		case "feedback_create", "feedback_capture", "guidance_seed":
			if c.Action == "feedback_capture" {
				// Chat submissions always go through a human reviewer.
				p.Risk = "medium"
				if p.FeedbackContext != nil {
					p.Negative = p.FeedbackContext.Rating == "incorrect"
				}
			}
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
				negatives, e := negativeFeedbackCount(ctx, tx, c.Scope, p.Topic)
				if e != nil {
					return Result{}, e
				}
				if negatives >= 2 {
					out.Status = "quarantined"
				}
				_, e = tx.ExecContext(ctx, `INSERT INTO ext_feedback_guidances VALUES(?,?,?,NULL,'seed',?,?,0.75,?,1,?)`, args(c.Scope, out.ID, p.Topic, p.Text, out.Status, s.now().UTC().Format(time.RFC3339Nano))...)
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
				negatives, e := negativeFeedbackCount(ctx, tx, c.Scope, p.Topic)
				if e != nil {
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
			negatives, e := negativeFeedbackCount(ctx, tx, c.Scope, p.Topic)
			if e != nil {
				return Result{}, e
			}
			if negatives < 2 {
				return Result{}, ErrConflict
			}
			_, e = tx.ExecContext(ctx, `UPDATE ext_feedback_guidances SET status='quarantined',revision=revision+1 WHERE tenant_id=? AND knowledge_base_id=? AND topic=? AND status='active'`, args(c.Scope, p.Topic)...)
			out.ID = p.Topic
			out.Status = "quarantined"
			return out, e
		case "feedback_topic_recover":
			if strings.TrimSpace(p.Topic) == "" || !p.SecondConfirmation {
				return Result{}, ErrConflict
			}
			negatives, e := negativeFeedbackCount(ctx, tx, c.Scope, p.Topic)
			if e != nil {
				return Result{}, e
			}
			if negatives < 2 {
				return Result{}, ErrConflict
			}
			id := newID()
			if id == "" {
				return Result{}, ErrUnavailable
			}
			if _, e := tx.ExecContext(ctx, `INSERT INTO ext_feedback_topic_recoveries
(tenant_id,knowledge_base_id,topic,recovery_id,reviewer,note,recovered_at) VALUES(?,?,?,?,?,?,?)`,
				append(args(c.Scope, p.Topic, id, c.ActorID, p.ReviewNote), s.now().UTC().Format(time.RFC3339Nano))...); e != nil {
				return Result{}, e
			}
			out.ID = p.Topic
			out.Status = "recovered"
			return out, nil
		}
		f, e := readQAFeedbackReview(ctx, tx, c)
		if e != nil && e != sql.ErrNoRows {
			return Result{}, e
		}
		if e == sql.ErrNoRows && (p.SupersedesCorrectionID != "" || p.SupersedesCorrectionRevision != 0) {
			return Result{}, ErrInvalid
		}
		if e == nil {
			switch c.Action {
			case "feedback_approve":
				return s.approveQAFeedback(ctx, tx, c, f)
			case "feedback_record_only", "feedback_needs_evidence", "feedback_reject":
				return s.recordQADisposition(ctx, tx, c, f, c.Action)
			case "feedback_activate":
				if err := s.currentQAPin(ctx, tx, c.Scope, f.anchor); err != nil {
					return Result{}, err
				}
			}
		}
		var status, risk, topic, text string
		var reviewContext bool
		var rev int64
		var ready sql.NullString
		if e := tx.QueryRowContext(ctx, `SELECT f.status,f.risk,f.topic,f.text,f.revision,f.ready_at,r.feedback_id IS NOT NULL
FROM ext_feedback_staging f LEFT JOIN ext_feedback_review_context r USING(tenant_id,knowledge_base_id,feedback_id)
WHERE f.tenant_id=? AND f.knowledge_base_id=? AND f.feedback_id=?`, args(c.Scope, c.ID)...).Scan(&status, &risk, &topic, &text, &rev, &ready, &reviewContext); e != nil {
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
			if reviewContext {
				reviewed := strings.TrimSpace(p.Text)
				if reviewed == "" || len(reviewed) > 720 {
					return Result{}, ErrInvalid
				}
				if _, e := tx.ExecContext(ctx, `UPDATE ext_feedback_review_context SET reviewed_guidance=?
WHERE tenant_id=? AND knowledge_base_id=? AND feedback_id=?`, append([]any{reviewed}, args(c.Scope, c.ID)...)...); e != nil {
					return Result{}, e
				}
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
			negatives, e := negativeFeedbackCount(ctx, tx, c.Scope, topic)
			if e != nil {
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
			if reviewContext {
				if e := tx.QueryRowContext(ctx, `SELECT reviewed_guidance FROM ext_feedback_review_context
WHERE tenant_id=? AND knowledge_base_id=? AND feedback_id=?`, args(c.Scope, c.ID)...).Scan(&text); e != nil || text == "" {
					return Result{}, ErrConflict
				}
			}
			if _, e = tx.ExecContext(ctx, `INSERT INTO ext_feedback_guidances VALUES(?,?,?,?,'feedback',?,?,0.75,'active',1,?)`, args(c.Scope, c.ID, c.ID, topic, text, s.now().UTC().Format(time.RFC3339Nano))...); e != nil {
				return Result{}, e
			}
			if risk == "low" && feedbackSampleSelected(c.ID) {
				if _, e = tx.ExecContext(ctx, `INSERT INTO ext_feedback_sample_reviews(tenant_id,knowledge_base_id,feedback_id,decision,reviewer,note,selected_at,reviewed_at)
VALUES(?,?,?, 'pending','','',?,NULL)`, args(c.Scope, c.ID, s.now().UTC().Format(time.RFC3339Nano))...); e != nil {
					return Result{}, e
				}
			}
		case "feedback_sample_review":
			if status != "active" || strings.TrimSpace(c.ActorID) == "" {
				return Result{}, ErrConflict
			}
			if p.SampleDecision != "passed" && p.SampleDecision != "failed" {
				return Result{}, ErrInvalid
			}
			if len(p.ReviewNote) > 2000 {
				return Result{}, ErrInvalid
			}
			var decision string
			if e := tx.QueryRowContext(ctx, `SELECT decision FROM ext_feedback_sample_reviews
WHERE tenant_id=? AND knowledge_base_id=? AND feedback_id=?`, args(c.Scope, c.ID)...).Scan(&decision); e != nil || decision != "pending" {
				return Result{}, ErrConflict
			}
			if _, e := tx.ExecContext(ctx, `UPDATE ext_feedback_sample_reviews
SET decision=?,reviewer=?,note=?,reviewed_at=? WHERE tenant_id=? AND knowledge_base_id=? AND feedback_id=? AND decision='pending'`,
				append([]any{p.SampleDecision, c.ActorID, p.ReviewNote, s.now().UTC().Format(time.RFC3339Nano)}, args(c.Scope, c.ID)...)...); e != nil {
				return Result{}, e
			}
			out.Status = status
			if p.SampleDecision == "failed" {
				out.Status = "quarantined"
				ready = sql.NullString{}
				if _, e := tx.ExecContext(ctx, `UPDATE ext_feedback_guidances SET status='quarantined',revision=revision+1
WHERE tenant_id=? AND knowledge_base_id=? AND feedback_id=? AND status='active'`, args(c.Scope, c.ID)...); e != nil {
					return Result{}, e
				}
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
		_, e = tx.ExecContext(ctx, `UPDATE ext_feedback_staging SET status=?,revision=?,ready_at=? WHERE tenant_id=? AND knowledge_base_id=? AND feedback_id=?`, append([]any{out.Status, out.Revision, ready}, args(c.Scope, c.ID)...)...)
		if e == nil && c.Action == "feedback_activate" && f.anchor != nil {
			e = s.publishCorrection(ctx, tx, c, f.anchor, s.now())
		}
		return out, e
	})
}

// List returns inspectable, scoped management records; it does not inject them into QA.
func (s *Service) List(ctx context.Context, scopeScope Command) ([]map[string]any, error) {
	c := scopeScope
	var table, idcol, fields string
	switch c.Action {
	case "feedback_list", "qa_feedback_list":
		return s.listFeedback(ctx, c)
	case "relation_list":
		table, idcol, fields = relationRecords, "relation_id", "details"
		if c.IncludeSupplementDeclarations || c.Payload.DocumentID != "" || c.ListEdgeType == "supplements" || c.ListStatus == "supplement_declared" {
			table = documentRelationRecords
		}
		if !validRelationListFilters(c.ListEdgeType, c.ListStatus) {
			return nil, ErrInvalid
		}
	case "guidance_list":
		table, idcol = "ext_feedback_guidances", "guidance_id"
		fields = `json_object('text',text,'topic',topic,'source',source,'confidence',confidence)`
	case "revision_list":
		table, idcol, fields = revisionRecords+" AS records", "proposal_id", "details"
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
	if c.Payload.DocumentID != "" && !validID(c.Payload.DocumentID) {
		return nil, ErrInvalid
	}
	e := s.db.View(ctx, func(tx *sql.Tx) error {
		where := "tenant_id=? AND knowledge_base_id=?"
		params := args(c.Scope)
		if c.Action == "relation_list" {
			where = "tenant_id=? AND (knowledge_base_id=? OR target_knowledge_base_id=?)"
			params = append(args(c.Scope), c.Scope.KnowledgeBaseID)
			if doc := c.Payload.DocumentID; doc != "" {
				where = "tenant_id=? AND ((knowledge_base_id=? AND document_id=?) OR (target_knowledge_base_id=? AND target_document_id=?))"
				params = args(c.Scope, doc, c.Scope.KnowledgeBaseID, doc)
			}
			if c.ListEdgeType != "" {
				where += " AND edge_type=?"
				params = append(params, c.ListEdgeType)
			}
			if c.ListStatus != "" {
				where += " AND status=?"
				params = append(params, c.ListStatus)
			}
		}
		if c.ID != "" {
			if !validID(c.ID) {
				return ErrInvalid
			}
			where += " AND " + idcol + "=?"
			params = append(params, c.ID)
		} else if c.Action == "revision_list" {
			where += " AND json_extract(details,'$.record_kind') <> 'split_part'"
		}
		if c.Payload.DocumentID != "" {
			switch c.Action {
			case "relation_list":
			case "revision_list":
				where += ` AND (json_extract(details,'$.source_document_id')=? OR json_extract(details,'$.target_document_id')=?
OR (json_extract(details,'$.record_kind')='split_batch' AND EXISTS (
 SELECT 1 FROM ext_revision_split_parts p JOIN ext_revision_proposals r USING(tenant_id,knowledge_base_id,proposal_id)
 WHERE p.tenant_id=records.tenant_id AND p.knowledge_base_id=records.knowledge_base_id
 AND p.batch_id=records.proposal_id AND r.status='published' AND r.target_document_id=?)))`
				params = append(params, c.Payload.DocumentID, c.Payload.DocumentID, c.Payload.DocumentID)
			default:
				return ErrInvalid
			}
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
		if err := rows.Err(); err != nil {
			return err
		}
		if err := rows.Close(); err != nil {
			return err
		}
		if c.Action == "relation_list" {
			return addRelationReviewReasons(ctx, tx, c.Scope.TenantID, s.now(), out)
		}
		if c.Action == "revision_list" && c.ID != "" && len(out) == 1 && out[0]["record_kind"] == "split_batch" {
			parts, err := listSplitParts(ctx, tx, c.Scope, c.ID)
			if err != nil {
				return err
			}
			out[0]["split_parts"] = parts
		}
		return nil
	})
	return out, e
}

// Attachment links are system records; their presence is not a claim that
// either document is currently retrievable.
const relationRecords = `(
SELECT tenant_id,knowledge_base_id,relation_id,status,revision,target_knowledge_base_id,document_id,target_document_id,edge_type,
json_object('knowledge_base_id',knowledge_base_id,'document_id',document_id,'target_knowledge_base_id',target_knowledge_base_id,'target_document_id',target_document_id,'edge_type',edge_type,'confidence',confidence,'source_version_id',source_version_id,'target_version_id',target_version_id) AS details
FROM ext_document_relations
UNION ALL
SELECT tenant_id,knowledge_base_id,notice_id,status,revision,knowledge_base_id,document_id,target_document_id,'repeals',
json_object('document_id',document_id,'target_knowledge_base_id',knowledge_base_id,'target_document_id',target_document_id,'edge_type','repeals',
 'source_version_id',source_version_id,'target_version_id',target_version_id,
 'effective_at',strftime('%Y-%m-%dT%H:%M:%fZ',effective_at_ms/1000.0,'unixepoch'),'applied_at',applied_at)
FROM ext_repeal_notices
UNION ALL
SELECT r.tenant_id,r.knowledge_base_id,r.relation_id,r.status,r.revision,r.knowledge_base_id,r.document_id,r.target_document_id,r.edge_type,
json_object('document_id',r.document_id,'target_knowledge_base_id',r.knowledge_base_id,'target_document_id',r.target_document_id,'edge_type',r.edge_type,
 'source_version_id',r.source_version_id,'target_version_id',r.target_version_id,
 'source_clause',json_object('chunk_id',r.source_chunk_id,'content_hash',r.source_content_hash),
 'target_clause',json_object('chunk_id',r.target_chunk_id,'content_hash',r.target_content_hash),
 'applies_when',r.applies_when,'resolution',r.resolution,'decision_note',r.decision_note,
 'resolved_by',r.resolved_by,'resolved_at',r.resolved_at,
 'pins_current',json(CASE WHEN v.relation_id IS NULL THEN 'false' ELSE 'true' END))
FROM ext_clause_relations r LEFT JOIN ext_clause_current v USING(tenant_id,knowledge_base_id,relation_id)
UNION ALL
SELECT a.tenant_id,a.knowledge_base_id,'attachment:'||a.attachment_id,
CASE WHEN a.removed_at IS NOT NULL THEN 'attachment_removed'
     WHEN d.document_id IS NULL AND a.retired_at IS NOT NULL THEN 'attachment_retired'
     WHEN d.document_id IS NULL THEN 'linked'
     WHEN a.cleanup_error<>'' THEN 'attachment_cleanup_retry'
     ELSE 'attachment_cleanup_pending' END,1,a.knowledge_base_id,a.attachment_id,a.parent_document_id,'attachment_of',
json_object('document_id',a.attachment_id,'target_knowledge_base_id',a.knowledge_base_id,'target_document_id',a.parent_document_id,'edge_type','attachment_of',
 'effective_from',ann.effective_from,'effective_until',ann.effective_until)
FROM ext_attachment_links a
LEFT JOIN ext_attachment_delete_intents d
ON d.tenant_id=a.tenant_id AND d.knowledge_base_id=a.knowledge_base_id AND d.document_id=a.parent_document_id
LEFT JOIN ext_document_annotations ann
ON ann.tenant_id=a.tenant_id AND ann.knowledge_base_id=a.knowledge_base_id AND ann.document_id=a.parent_document_id
WHERE a.attachment_id IS NOT NULL
)`

// Supplement declarations are visible from either endpoint in the document workbench.
// They are edited through the supplement workflow and do not grant retrieval permission.
const documentRelationRecords = `(
SELECT * FROM ` + relationRecords + `
UNION ALL
SELECT d.tenant_id,d.knowledge_base_id,'supplement:'||f.edge_id,'supplement_declared',d.revision,d.knowledge_base_id,d.document_id,p.parent_document_id,'supplements',
 json_object('knowledge_base_id',d.knowledge_base_id,'document_id',d.document_id,'target_knowledge_base_id',d.knowledge_base_id,'target_document_id',p.parent_document_id,'edge_type','supplements',
 'source_version_id',d.child_version_node_id,'target_version_id',p.parent_version_node_id)
FROM ext_supplement_current c
JOIN ext_supplement_declarations d USING(tenant_id,knowledge_base_id,document_id,revision,declaration_id)
JOIN ext_supplement_parents p USING(tenant_id,knowledge_base_id,document_id,declaration_id)
JOIN ext_supplement_edge_facts f USING(tenant_id,knowledge_base_id,document_id,declaration_id,parent_document_id)
WHERE d.status='active'
UNION ALL
SELECT d.tenant_id,d.knowledge_base_id,'supplement:'||cf.cross_edge_id,'supplement_declared',d.revision,cp.parent_knowledge_base_id,d.document_id,cp.parent_document_id,'supplements',
 json_object('knowledge_base_id',d.knowledge_base_id,'document_id',d.document_id,'target_knowledge_base_id',cp.parent_knowledge_base_id,'target_document_id',cp.parent_document_id,'edge_type','supplements',
 'source_version_id',d.child_version_node_id,'target_version_id',cp.parent_version_node_id)
FROM ext_supplement_current c
JOIN ext_supplement_declarations d ON d.tenant_id=c.tenant_id AND d.knowledge_base_id=c.knowledge_base_id AND d.document_id=c.document_id AND d.declaration_id=c.declaration_id AND d.revision=c.revision
JOIN ext_supplement_cross_parents cp ON cp.declaration_id=d.declaration_id AND cp.tenant_id=d.tenant_id AND cp.knowledge_base_id=d.knowledge_base_id AND cp.document_id=d.document_id
JOIN ext_supplement_cross_edge_facts cf ON cf.declaration_id=cp.declaration_id AND cf.parent_knowledge_base_id=cp.parent_knowledge_base_id AND cf.parent_document_id=cp.parent_document_id
WHERE d.status='active'
)`
