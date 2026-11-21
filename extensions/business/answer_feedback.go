package business

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ybochuan/LanRAG-Next/extensions/corrections"
	"github.com/ybochuan/LanRAG-Next/extensions/provenance"
)

const maxFeedbackCitations = 100

// FeedbackCitation identifies a cited chunk that a successful step actually consumed.
type FeedbackCitation struct {
	ClauseAnchor
	DocumentID string `json:"document_id"`
	VersionID  string `json:"version_id"`
	ChunkIndex int    `json:"chunk_index"`
}

func feedbackOwner(c Command) (string, bool) {
	tenant, owner, ok := strings.Cut(c.ActorID, ":")
	return owner, ok && tenant == strconv.FormatUint(c.Scope.TenantID, 10) && validID(owner)
}

// SubmitQAFeedback stores one rating for a native-authorized answer. The caller
// decides whether the candidate KB is configured for this Host.
func (s *Service) SubmitQAFeedback(ctx context.Context, c Command, candidateConfigured bool) (Result, error) {
	owner, ok := feedbackOwner(c)
	p := c.Payload
	if !ok || c.Action != "qa_feedback_submit" || c.ExpectedRevision != 0 || c.ID != "" ||
		!validID(p.SessionID) || !validID(p.MessageID) || p.FeedbackContext == nil ||
		(c.Scope.KnowledgeBaseID != "" && !validID(c.Scope.KnowledgeBaseID)) {
		return Result{}, ErrInvalid
	}
	details := *p.FeedbackContext
	if !validFeedbackText(p.Text, 16000) || !validFeedbackText(details.Question, 2048) ||
		!validFeedbackText(details.AnswerExcerpt, 8192) || details.SuggestedRevision != "" ||
		len(p.CitedChunkIDs) > maxFeedbackCitations {
		return Result{}, ErrInvalid
	}
	switch p.Reason {
	case "", "inaccurate", "incomplete", "missing_sources", "other":
	default:
		return Result{}, ErrInvalid
	}
	for _, id := range p.CitedChunkIDs {
		if !validID(id) {
			return Result{}, ErrInvalid
		}
	}
	p.Text = strings.TrimSpace(p.Text)
	commented := p.Text != "" || details.SnapshotRestricted && details.CommentPresent
	status := "pending_review"
	switch details.Rating {
	case "like":
		status = "received"
	case "dislike":
		if !commented {
			status = "received"
		}
	case "correction", "supplement":
		if !commented {
			return Result{}, ErrInvalid
		}
	default:
		return Result{}, ErrInvalid
	}

	// Scope and the answer snapshot are server-derived. A retry stays bound to
	// the owner, message and submitted rating even if the candidate KB changes.
	candidate := c.Scope.KnowledgeBaseID
	c.Scope.KnowledgeBaseID = ""
	receiptText := p.Text
	if details.SnapshotRestricted {
		receiptText = ""
	}
	c.Payload = Payload{SessionID: p.SessionID, MessageID: p.MessageID, Text: receiptText, Reason: p.Reason,
		FeedbackContext: &FeedbackContext{Rating: details.Rating, CommentPresent: commented}}
	return s.transact(ctx, c, func(tx *sql.Tx) (Result, error) {
		scope := c.Scope
		citations := []FeedbackCitation{}
		restricted := details.SnapshotRestricted || candidate != "" && !candidateConfigured
		if !restricted && candidate != "" {
			run, state, safe, err := qaFeedbackRun(ctx, tx, scope, owner, p.SessionID, p.MessageID, candidate)
			if err != nil {
				return Result{}, err
			}
			restricted = !safe
			if safe {
				scope.KnowledgeBaseID = candidate
				if state == "sealed" && len(p.CitedChunkIDs) != 0 {
					citations, err = qaFeedbackCitations(ctx, tx, scope, run, p.CitedChunkIDs)
					if err != nil {
						return Result{}, err
					}
				}
			}
		}
		details.CommentPresent = commented
		if restricted {
			details = FeedbackContext{Rating: details.Rating, SnapshotRestricted: true, CommentPresent: commented}
			p.Text = ""
		}
		id := newID()
		if id == "" {
			return Result{}, ErrUnavailable
		}
		snapshot, _ := json.Marshal(details)
		chunks, _ := json.Marshal(citations)
		_, err := tx.ExecContext(ctx, `INSERT INTO ext_qa_feedback
   (tenant_id,knowledge_base_id,feedback_id,submitted_at,rating,reason,text,submitted_context,citation_chunks,status,revision)
   VALUES(?,?,?,?,?,?,?,?,?,?,1)`, args(scope, id, s.now().UTC().Format(time.RFC3339Nano), details.Rating,
			p.Reason, p.Text, string(snapshot), string(chunks), status)...)
		return Result{ID: id, Status: status, Revision: 1}, err
	})
}

func validFeedbackText(text string, limit int) bool {
	return len(text) <= limit && utf8.ValidString(text) && !strings.ContainsRune(text, 0)
}

// Native authorization identifies the candidate KB. When the answer has
// persisted runs, they must agree with that candidate before we show its snapshot.
func qaFeedbackRun(ctx context.Context, tx *sql.Tx, scope provenance.Scope, owner, session, message, candidate string) (run, state string, safe bool, err error) {
	var kb string
	var count int
	err = tx.QueryRowContext(ctx, `SELECT count(*),coalesce(max(knowledge_base_id),''),coalesce(max(run_id),''),coalesce(max(state),'')
  FROM (SELECT knowledge_base_id,run_id,state FROM ext_provenance_runs WHERE tenant_id=?
  AND json_extract(payload_json,'$.identity.user_id')=? AND json_extract(payload_json,'$.identity.session_id')=?
  AND json_extract(payload_json,'$.identity.attempt_id')=? LIMIT 2)`,
		strconv.FormatUint(scope.TenantID, 10), owner, session, message).Scan(&count, &kb, &run, &state)
	return run, state, count == 0 || count == 1 && kb == candidate, err
}

func qaFeedbackCitations(ctx context.Context, tx *sql.Tx, scope provenance.Scope, run string, cited []string) ([]FeedbackCitation, error) {
	ids, _ := json.Marshal(cited)
	rows, err := tx.QueryContext(ctx, `WITH matches AS (
  SELECT DISTINCT i.retrieved_chunk_id AS chunk_id,v.document_id,v.version_node_id,
   json_extract(leaf.value,'$.content_hash') AS content_hash,json_extract(leaf.value,'$.chunk_index') AS chunk_index
  FROM ext_provenance_inputs i JOIN ext_provenance_steps s USING(tenant_id,knowledge_base_id,run_id,step_id)
  JOIN json_each(i.refs_json) ref
  JOIN ext_document_version v ON v.tenant_id=i.tenant_id AND v.knowledge_base_id=i.knowledge_base_id
   AND v.document_id=json_extract(ref.value,'$.document_id') AND v.version_node_id=json_extract(ref.value,'$.version_id')
  JOIN ext_source_snapshot_evidence e ON e.tenant_id=v.tenant_id AND e.knowledge_base_id=v.knowledge_base_id AND e.version_node_id=v.version_node_id
  JOIN json_each(e.snapshot_json,'$.chunks') leaf ON json_extract(leaf.value,'$.id')=i.retrieved_chunk_id
  WHERE i.tenant_id=? AND i.knowledge_base_id=? AND i.run_id=? AND s.state='success' AND s.kind IN ('model','tool')
   AND i.retrieved_chunk_id<>'' AND i.retrieved_chunk_id IN (SELECT value FROM json_each(?))
 ) SELECT chunk_id,document_id,version_node_id,content_hash,chunk_index FROM matches
 WHERE chunk_id IN (SELECT chunk_id FROM matches GROUP BY chunk_id HAVING count(*)=1)
 ORDER BY document_id,chunk_index,chunk_id`, args(scope, run, string(ids))...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []FeedbackCitation{}
	for rows.Next() {
		var pin FeedbackCitation
		if err = rows.Scan(&pin.ChunkID, &pin.DocumentID, &pin.VersionID, &pin.ContentHash, &pin.ChunkIndex); err != nil {
			return nil, err
		}
		if pin.ClauseAnchor.valid() && validID(pin.DocumentID) && validID(pin.VersionID) && pin.ChunkIndex >= 0 {
			out = append(out, pin)
		}
	}
	return out, rows.Err()
}

type qaFeedbackReview struct {
	status    string
	revision  int64
	citations []FeedbackCitation
	anchor    *FeedbackCitation
}

func readQAFeedbackReview(ctx context.Context, tx *sql.Tx, c Command) (qaFeedbackReview, error) {
	var f qaFeedbackReview
	var citations []byte
	var anchor sql.NullString
	err := tx.QueryRowContext(ctx, `SELECT q.status,COALESCE(f.revision,q.revision),q.citation_chunks,q.feedback_anchor
 FROM ext_qa_feedback q LEFT JOIN ext_feedback_staging f USING(tenant_id,knowledge_base_id,feedback_id)
 WHERE q.tenant_id=? AND q.knowledge_base_id=? AND q.feedback_id=?`, args(c.Scope, c.ID)...).
		Scan(&f.status, &f.revision, &citations, &anchor)
	if err != nil {
		return f, err
	}
	if json.Unmarshal(citations, &f.citations) != nil || anchor.Valid && json.Unmarshal([]byte(anchor.String), &f.anchor) != nil {
		return f, ErrUnavailable
	}
	return f, nil
}

func (f qaFeedbackReview) approvalPin(c Command) (*FeedbackCitation, error) {
	p := c.Payload
	if !p.SourceClause.valid() || strings.TrimSpace(p.Text) == "" || strings.TrimSpace(p.AppliesWhen) == "" ||
		!validFeedbackText(p.Text, 720) || !validFeedbackText(p.AppliesWhen, 720) || len(guidanceInstruction(p.Text, p.AppliesWhen)) > 720 {
		return nil, ErrInvalid
	}
	if f.status != "pending_review" && f.status != "needs_evidence" {
		return nil, ErrConflict
	}
	for i := range f.citations {
		if f.citations[i].ClauseAnchor == *p.SourceClause {
			return &f.citations[i], nil
		}
	}
	return nil, ErrConflict
}

func guidanceInstruction(text, appliesWhen string) string {
	return "适用条件：" + strings.TrimSpace(appliesWhen) + "\n  " + strings.TrimSpace(text)
}

// Preflight checks live native evidence outside the single writer. The same
// exact pin and current extension truth are checked again in the transaction.
func (s *Service) prepareQAFeedbackAction(ctx context.Context, c Command) error {
	var f qaFeedbackReview
	err := s.db.View(ctx, func(tx *sql.Tx) error {
		var err error
		f, err = readQAFeedbackReview(ctx, tx, c)
		return err
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil // legacy feedback
	}
	if err != nil {
		return err
	}
	if f.revision != c.ExpectedRevision {
		return ErrConflict
	}
	pin := f.anchor
	if c.Action == "feedback_approve" {
		pin, err = f.approvalPin(c)
		if err != nil {
			return err
		}
	}
	if pin == nil {
		return ErrConflict
	}
	version, err := s.verify(ctx, c.Scope, pin.DocumentID)
	if errors.Is(err, ErrUnavailable) {
		return err
	}
	if err != nil || version != pin.VersionID {
		return ErrConflict
	}
	return nil
}

func (s *Service) currentQAPin(ctx context.Context, tx *sql.Tx, scope provenance.Scope, pin *FeedbackCitation) error {
	if pin == nil {
		return ErrConflict
	}
	if err := s.current(ctx, tx, scope, pin.DocumentID, pin.VersionID); err != nil {
		return err
	}
	var found bool
	err := tx.QueryRowContext(ctx, `SELECT EXISTS(
  SELECT 1 FROM ext_source_snapshot_evidence e JOIN json_each(e.snapshot_json,'$.chunks') leaf
  WHERE e.tenant_id=? AND e.knowledge_base_id=? AND e.version_node_id=?
   AND json_extract(leaf.value,'$.id')=? AND json_extract(leaf.value,'$.content_hash')=? AND json_extract(leaf.value,'$.chunk_index')=?
  ) AND NOT EXISTS (SELECT 1 FROM ext_document_availability
     WHERE tenant_id=? AND knowledge_base_id=? AND document_id=? AND status<>'active')`,
		append(args(scope, pin.VersionID, pin.ChunkID, pin.ContentHash, pin.ChunkIndex), args(scope, pin.DocumentID)...)...).Scan(&found)
	if err != nil {
		return err
	}
	if !found {
		return ErrConflict
	}
	return nil
}

// ReviewQAInbox records a tenant admin's disposition for an unscoped row.
func (s *Service) ReviewQAInbox(ctx context.Context, c Command) (Result, error) {
	if _, ok := feedbackOwner(c); !ok || c.Scope.KnowledgeBaseID != "" || c.Action != "qa_feedback_review" || !validID(c.ID) {
		return Result{}, ErrInvalid
	}
	switch c.Payload.Decision {
	case "feedback_record_only", "feedback_needs_evidence", "feedback_reject":
	default:
		return Result{}, ErrInvalid
	}
	return s.transact(ctx, c, func(tx *sql.Tx) (Result, error) {
		f, err := readQAFeedbackReview(ctx, tx, c)
		if errors.Is(err, sql.ErrNoRows) {
			return Result{}, ErrConflict
		}
		if err != nil {
			return Result{}, err
		}
		return s.recordQADisposition(ctx, tx, c, f, c.Payload.Decision)
	})
}

func (s *Service) recordQADisposition(ctx context.Context, tx *sql.Tx, c Command, f qaFeedbackReview, action string) (Result, error) {
	if f.revision != c.ExpectedRevision || (f.status != "pending_review" && f.status != "needs_evidence" && f.status != "received") {
		return Result{}, ErrConflict
	}
	var status string
	switch action {
	case "feedback_record_only":
		status = "record_only"
	case "feedback_needs_evidence":
		status = "needs_evidence"
	case "feedback_reject":
		status = "rejected"
	default:
		return Result{}, ErrInvalid
	}
	if status == f.status {
		return Result{}, ErrConflict
	}
	out := Result{ID: c.ID, Status: status, Revision: f.revision + 1}
	_, err := tx.ExecContext(ctx, `UPDATE ext_qa_feedback SET status=?,revision=? WHERE tenant_id=? AND knowledge_base_id=? AND feedback_id=?`,
		append([]any{out.Status, out.Revision}, args(c.Scope, c.ID)...)...)
	return out, err
}

func (s *Service) approveQAFeedback(ctx context.Context, tx *sql.Tx, c Command, f qaFeedbackReview) (Result, error) {
	if f.revision != c.ExpectedRevision {
		return Result{}, ErrConflict
	}
	pin, err := f.approvalPin(c)
	if err != nil {
		return Result{}, err
	}
	if err = s.currentQAPin(ctx, tx, c.Scope, pin); err != nil {
		return Result{}, err
	}
	if err = s.stageCorrectionReplacement(ctx, tx, c, pin); err != nil {
		return Result{}, err
	}
	out := Result{ID: c.ID, Status: "approved", Revision: f.revision + 1}
	text, condition := strings.TrimSpace(c.Payload.Text), strings.TrimSpace(c.Payload.AppliesWhen)
	project := strings.Join(strings.Fields(c.Payload.AppliesProject), " ")
	if !validFeedbackText(project, 160) || project != "" && utf8.RuneCountInString(project) < 2 {
		return Result{}, ErrInvalid
	}
	if err := s.verifyReviewedCorrectionPeers(ctx, tx, c, pin); err != nil {
		return Result{}, err
	}
	topic := "answer-feedback:" + c.ID
	now := s.now()
	if _, err = tx.ExecContext(ctx, `INSERT INTO ext_feedback_events VALUES(?,?,?,'manual',?,?,0,?)`,
		args(c.Scope, c.ID, topic, "", now.UTC().Format(time.RFC3339Nano))...); err != nil {
		return Result{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO ext_feedback_staging VALUES(?,?,?,?,?,'medium','approved',?,?)`,
		args(c.Scope, c.ID, topic, guidanceInstruction(text, condition), out.Revision, now.Add(48*time.Hour).UTC().Format(time.RFC3339Nano))...); err != nil {
		return Result{}, err
	}
	body, err := json.Marshal(struct {
		*FeedbackCitation
		AppliesProject string             `json:"applies_project,omitempty"`
		ReviewedPeers  []corrections.Peer `json:"reviewed_peers,omitempty"`
	}{pin, project, c.Payload.ReviewedCorrectionPeers})
	if err != nil {
		return Result{}, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE ext_qa_feedback SET status=?,revision=?,feedback_anchor=?,applies_when=?,reviewed_guidance=?
  WHERE tenant_id=? AND knowledge_base_id=? AND feedback_id=?`,
		append([]any{out.Status, out.Revision, string(body), condition, text}, args(c.Scope, c.ID)...)...)
	return out, err
}
