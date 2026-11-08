package business

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/ybochuan/LanRAG-Next/extensions/outbox"
	"github.com/ybochuan/LanRAG-Next/extensions/provenance"
	"github.com/ybochuan/LanRAG-Next/extensions/store"
)

// SplitPartInput is one complete, independently publishable Markdown document.
type SplitPartInput struct {
	Title string `json:"title"`
	Text  string `json:"text"`
}

type splitRemote interface {
	Remote
	ValidateSplitSource(context.Context, Publication) error
	CreateDraft(context.Context, Publication) (RemoteDocument, error)
	IndexDraft(context.Context, Publication) (RemoteDocument, error)
}

// An approved split requires exact Markdown bytes. Refuse inputs the native
// manual endpoint can reject or rewrite, including tokens inside fenced code.
var incompatibleManualMarkdown = regexp.MustCompile(`(?i)<(?:script|iframe|object|embed|form|input|button)|(?:javascript|vbscript):|on(?:load|error|click|mouseover|focus|blur)\s*=`)

func normalizeSplitParts(input []SplitPartInput) ([]SplitPartInput, error) {
	if len(input) < 2 || len(input) > 8 {
		return nil, ErrInvalid
	}
	titles := make(map[string]bool, len(input))
	parts := make([]SplitPartInput, 0, len(input))
	total := 0
	for _, p := range input {
		p.Title = strings.TrimSpace(p.Title)
		p.Text = strings.TrimSpace(strings.ReplaceAll(p.Text, "\r\n", "\n"))
		key := strings.ToLower(p.Title)
		if p.Title == "" || len(p.Title) > 120 || strings.IndexFunc(p.Title, func(r rune) bool { return r < 32 }) >= 0 ||
			p.Text == "" || titles[key] || !utf8.ValidString(p.Title) || !utf8.ValidString(p.Text) ||
			incompatibleManualMarkdown.MatchString(p.Title) || incompatibleManualMarkdown.MatchString(p.Text) {
			return nil, ErrInvalid
		}
		titles[key] = true
		p.Text += "\n"
		total += len(p.Text)
		parts = append(parts, p)
	}
	if total > 48000 {
		return nil, ErrInvalid
	}
	return parts, nil
}

func (s *Service) split(ctx context.Context, c Command) (Result, error) {
	if s == nil || s.db == nil {
		return Result{}, ErrUnavailable
	}
	if c.Scope.TenantID == 0 || !validID(c.Scope.KnowledgeBaseID) || !validID(c.ActorID) || !validID(c.OperationID) || c.ExpectedRevision < 0 {
		return Result{}, ErrInvalid
	}
	if result, replayed, err := s.replay(ctx, c); replayed || err != nil {
		return result, err
	}

	if c.Action == "split_create" {
		return s.createSplit(ctx, c)
	}
	if !validID(c.ID) {
		return Result{}, ErrInvalid
	}
	var source, version, observedStatus string
	var observedRevision int64
	if c.Action == "split_approve" {
		remote, ok := s.remote.(splitRemote)
		if !ok {
			return Result{}, ErrUnavailable
		}
		err := s.db.View(ctx, func(tx *sql.Tx) error {
			return tx.QueryRowContext(ctx, `SELECT source_document_id,source_version_id,status,revision FROM ext_revision_split_batches
				WHERE tenant_id=? AND knowledge_base_id=? AND batch_id=?`,
				args(c.Scope, c.ID)...).Scan(&source, &version, &observedStatus, &observedRevision)
		})
		if errors.Is(err, sql.ErrNoRows) {
			return Result{}, ErrConflict
		}
		if err != nil {
			return Result{}, err
		}
		if observedStatus == "pending_review" && observedRevision == c.ExpectedRevision {
			if current, err := s.verify(ctx, c.Scope, source); err != nil || current != version {
				return Result{}, ErrConflict
			}
			if err := remote.ValidateSplitSource(ctx, Publication{Scope: c.Scope, SourceDocumentID: source}); err != nil {
				if errors.Is(err, ErrBinarySource) {
					return Result{}, ErrInvalid
				}
				return Result{}, err
			}
		}
	}
	return s.transact(ctx, c, func(tx *sql.Tx) (Result, error) {
		status := "rejected"
		if c.Action == "split_approve" {
			if observedStatus != "pending_review" || observedRevision != c.ExpectedRevision {
				return Result{}, ErrConflict
			}
			if err := s.current(ctx, tx, c.Scope, source, version); err != nil {
				return Result{}, err
			}
			if err := noApprovedSplit(ctx, tx, c.Scope, source, version); err != nil {
				return Result{}, err
			}
			var publishing bool
			if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM ext_revision_proposals r
				WHERE r.tenant_id=? AND r.knowledge_base_id=? AND r.source_document_id=? AND r.source_version_id=?
				AND (r.status IN ('publishing','indexed') OR r.status='publish_failed' AND r.publish_error<>'binary_source_upload_required')
				AND NOT EXISTS(SELECT 1 FROM ext_revision_split_parts p WHERE p.tenant_id=r.tenant_id
				AND p.knowledge_base_id=r.knowledge_base_id AND p.proposal_id=r.proposal_id))`,
				args(c.Scope, source, version)...).Scan(&publishing); err != nil {
				return Result{}, err
			}
			if publishing {
				return Result{}, ErrConflict
			}
			status = "approved"
		}
		res, err := tx.ExecContext(ctx, `UPDATE ext_revision_split_batches SET status=?,revision=revision+1
			WHERE tenant_id=? AND knowledge_base_id=? AND batch_id=? AND status='pending_review' AND revision=?`,
			append([]any{status}, args(c.Scope, c.ID, c.ExpectedRevision)...)...)
		if err != nil {
			return Result{}, err
		}
		if n, err := res.RowsAffected(); err != nil || n != 1 {
			return Result{}, ErrConflict
		}
		_, err = tx.ExecContext(ctx, `UPDATE ext_revision_proposals SET status=?,revision=revision+1
			WHERE tenant_id=? AND knowledge_base_id=? AND status='pending_review' AND proposal_id IN (
				SELECT proposal_id FROM ext_revision_split_parts WHERE tenant_id=? AND knowledge_base_id=? AND batch_id=?
			)`, append(append([]any{status}, args(c.Scope)...), args(c.Scope, c.ID)...)...)
		return Result{ID: c.ID, Status: status, Revision: c.ExpectedRevision + 1}, err
	})
}

func (s *Service) createSplit(ctx context.Context, c Command) (Result, error) {
	parts, err := normalizeSplitParts(c.Payload.SplitParts)
	if err != nil || !validID(c.Payload.DocumentID) || c.ExpectedRevision != 0 ||
		len(c.Payload.GuidanceIDs) == 0 || len(c.Payload.GuidanceIDs) > 3 {
		return Result{}, ErrInvalid
	}
	source, err := s.verify(ctx, c.Scope, c.Payload.DocumentID)
	if err != nil {
		return Result{}, err
	}
	return s.transact(ctx, c, func(tx *sql.Tx) (Result, error) {
		if err := s.current(ctx, tx, c.Scope, c.Payload.DocumentID, source); err != nil {
			return Result{}, err
		}
		if err := noApprovedSplit(ctx, tx, c.Scope, c.Payload.DocumentID, source); err != nil {
			return Result{}, err
		}
		if err := requireRevisionGuidances(ctx, tx, c.Scope, c.Payload.GuidanceIDs); err != nil {
			return Result{}, err
		}
		var family string
		if err := tx.QueryRowContext(ctx, `SELECT family_id FROM ext_document_version_family
			WHERE tenant_id=? AND knowledge_base_id=? AND document_id=?`, args(c.Scope, c.Payload.DocumentID)...).Scan(&family); err != nil {
			return Result{}, err
		}
		batch := newID()
		if batch == "" {
			return Result{}, ErrUnavailable
		}
		now := s.now().UTC().Format(publicationTime)
		_, err := tx.ExecContext(ctx, `INSERT INTO ext_revision_split_batches(
			tenant_id,knowledge_base_id,batch_id,source_document_id,source_version_id,status,revision,created_at
		) VALUES(?,?,?,?,?,'pending_review',1,?)`, args(c.Scope, batch, c.Payload.DocumentID, source, now)...)
		if err != nil {
			return Result{}, err
		}
		guidance, _ := json.Marshal(c.Payload.GuidanceIDs)
		for position, part := range parts {
			id := newID()
			if id == "" {
				return Result{}, ErrUnavailable
			}
			hash := textHash(part.Text)
			_, err := tx.ExecContext(ctx, `INSERT INTO ext_revision_proposals(
				tenant_id,knowledge_base_id,proposal_id,source_document_id,source_version_id,family_id,
				trigger_guidance_ids,proposed_patch,target_content_hash,status,revision,publication_key,created_at
			) VALUES(?,?,?,?,?,?,?,?,?,'pending_review',1,?,?)`,
				args(c.Scope, id, c.Payload.DocumentID, source, family, string(guidance), part.Text, hash, textHash(id+source+hash), now)...)
			if err != nil {
				return Result{}, err
			}
			_, err = tx.ExecContext(ctx, `INSERT INTO ext_revision_split_parts(
				tenant_id,knowledge_base_id,batch_id,proposal_id,position,title
			) VALUES(?,?,?,?,?,?)`, args(c.Scope, batch, id, position+1, part.Title)...)
			if err != nil {
				return Result{}, err
			}
		}
		return Result{ID: batch, Status: "pending_review", Revision: 1}, nil
	})
}

func noApprovedSplit(ctx context.Context, tx *sql.Tx, scope provenance.Scope, doc, version string) error {
	var reserved bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM ext_revision_split_batches
		WHERE tenant_id=? AND knowledge_base_id=? AND source_document_id=? AND source_version_id=? AND status='approved')`,
		args(scope, doc, version)...).Scan(&reserved); err != nil {
		return err
	}
	if reserved {
		return ErrConflict
	}
	return nil
}

// A superseded source is accepted only for a member of the batch that retired it.
func (s *Service) publicationSource(ctx context.Context, tx *sql.Tx, p Publication) (first bool, err error) {
	if p.SplitID == "" {
		if err := noApprovedSplit(ctx, tx, p.Scope, p.SourceDocumentID, p.SourceVersionID); err != nil {
			return false, err
		}
		return false, s.current(ctx, tx, p.Scope, p.SourceDocumentID, p.SourceVersionID)
	}
	var firstProposal sql.NullString
	if err := tx.QueryRowContext(ctx, `SELECT b.first_proposal_id FROM ext_revision_split_batches b
		JOIN ext_revision_split_parts m USING(tenant_id,knowledge_base_id,batch_id)
		WHERE b.tenant_id=? AND b.knowledge_base_id=? AND b.batch_id=? AND m.proposal_id=?
		AND b.status='approved' AND b.source_document_id=? AND b.source_version_id=?`,
		args(p.Scope, p.SplitID, p.ProposalID, p.SourceDocumentID, p.SourceVersionID)...).Scan(&firstProposal); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, ErrConflict
		}
		return false, err
	}
	if !firstProposal.Valid {
		return true, s.current(ctx, tx, p.Scope, p.SourceDocumentID, p.SourceVersionID)
	}
	state, event, err := store.ReadLifecycleCurrentTx(ctx, tx, p.Scope.TenantID, p.Scope.KnowledgeBaseID, p.SourceDocumentID)
	if err != nil || state != "superseded" || event != "split-"+p.SplitID {
		return false, ErrConflict
	}
	var head string
	if err := tx.QueryRowContext(ctx, `SELECT current_version_node_id FROM ext_document_version_family
		WHERE tenant_id=? AND knowledge_base_id=? AND document_id=?`, args(p.Scope, p.SourceDocumentID)...).Scan(&head); err != nil || head != p.SourceVersionID {
		return false, ErrConflict
	}
	return false, nil
}

func (s *Service) observeVersion(ctx context.Context, scope provenance.Scope, doc string) (string, error) {
	if s.registry == nil {
		return s.verify(ctx, scope, doc)
	}
	v, err := s.registry.Refresh(ctx, scope, doc)
	return v.ID, err
}

func (s *Service) publishSplit(ctx, remoteCtx context.Context, p Publication, r Result) (Result, error) {
	remote, ok := s.remote.(splitRemote)
	if !ok {
		return Result{}, ErrUnavailable
	}
	target, err := remote.Reconcile(remoteCtx, p)
	if err != nil {
		return s.publishFailure(ctx, p, r, err)
	}
	if p.TargetDocumentID == "" {
		if target.ID == "" {
			target, err = remote.CreateDraft(remoteCtx, p)
			if err != nil {
				return s.publishFailure(ctx, p, r, err)
			}
			if !target.Draft {
				return s.publishMismatch(ctx, p, r)
			}
		}
		if !validID(target.ID) || target.ID == p.SourceDocumentID || target.ContentHash != p.ContentHash {
			return s.publishMismatch(ctx, p, r)
		}
		// The native channel already fences unbound documents. Bind the ID
		// before this publisher can submit indexing or finalize the split.
		return s.publishState(ctx, p, r, "publishing", "", target.ID, "")
	}
	if target.ID != p.TargetDocumentID || target.ContentHash != p.ContentHash {
		return s.publishMismatch(ctx, p, r)
	}
	if target.Draft || target.IndexFailed {
		target, err = remote.IndexDraft(remoteCtx, p)
		if err != nil {
			return s.publishFailure(ctx, p, r, err)
		}
		if target.Draft || target.ID != p.TargetDocumentID || target.ContentHash != p.ContentHash {
			return s.publishMismatch(ctx, p, r)
		}
		return s.publishState(ctx, p, r, "publishing", "", target.ID, "")
	}
	if !target.Indexed {
		return s.publishState(ctx, p, r, "publishing", "", target.ID, "")
	}
	r, err = s.publishState(ctx, p, r, "indexed", "", target.ID, "")
	if err != nil {
		return Result{}, err
	}
	version, err := s.observeVersion(ctx, p.Scope, target.ID)
	if err != nil {
		return r, nil // The observer may not have initialized the target yet.
	}
	if head, err := s.observeVersion(ctx, p.Scope, p.SourceDocumentID); err != nil || head != p.SourceVersionID {
		return Result{}, ErrConflict
	}
	p.TargetMetadataHash = target.MetadataHash
	return s.publishState(ctx, p, r, "published", "", target.ID, version)
}

func (s *Service) finalizeSplit(ctx context.Context, tx *sql.Tx, p Publication, target, version string, first bool) (string, error) {
	if target != p.TargetDocumentID || target == p.SourceDocumentID {
		return "", ErrConflict
	}
	var family, metadata string
	err := tx.QueryRowContext(ctx, `SELECT f.family_id,COALESCE(json_extract(e.snapshot_json,'$.header_fields.metadata'),'')
		FROM ext_document_version_family f LEFT JOIN ext_source_snapshot_evidence e
		ON e.tenant_id=f.tenant_id AND e.knowledge_base_id=f.knowledge_base_id AND e.version_node_id=f.current_version_node_id
		WHERE f.tenant_id=? AND f.knowledge_base_id=? AND f.document_id=? AND f.current_version_node_id=?`,
		args(p.Scope, target, version)...).Scan(&family, &metadata)
	if err != nil {
		return "", err
	}
	if metadata == "" || metadata != p.TargetMetadataHash || family == p.FamilyID {
		return "", ErrConflict
	}
	now := s.now().UTC()
	if first {
		if err := s.retireRevisionSource(ctx, tx, p, "split-"+p.SplitID, "split_first_published", now); err != nil {
			return "", err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE ext_revision_split_batches SET first_proposal_id=?
			WHERE tenant_id=? AND knowledge_base_id=? AND batch_id=? AND first_proposal_id IS NULL`,
			append([]any{p.ProposalID}, args(p.Scope, p.SplitID)...)...); err != nil {
			return "", err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE ext_revision_split_parts SET target_version_id=?
		WHERE tenant_id=? AND knowledge_base_id=? AND batch_id=? AND proposal_id=?`,
		append([]any{version}, args(p.Scope, p.SplitID, p.ProposalID)...)...); err != nil {
		return "", err
	}
	payload, _ := json.Marshal(map[string]string{"document_id": target, "family_id": family})
	if err := outbox.EnqueueTx(ctx, tx, outbox.Item{ID: "family:" + p.Key + ":" + target, TenantID: p.Scope.TenantID,
		KnowledgeBaseID: p.Scope.KnowledgeBaseID, ProjectionType: "family_tag", AggregateType: "document", AggregateID: target,
		CausationType: "document_version", CausationID: version, Payload: payload, CreatedAt: now}); err != nil {
		return "", err
	}
	return family, nil
}

const revisionRecords = `(
SELECT r.tenant_id,r.knowledge_base_id,r.proposal_id,r.status,r.revision,
	json_object('record_kind',CASE WHEN m.proposal_id IS NULL THEN 'revision' ELSE 'split_part' END,
	'split_id',COALESCE(m.batch_id,''),'split_title',COALESCE(m.title,''),
	'proposed_patch',r.proposed_patch,'source_document_id',r.source_document_id,'source_version_id',r.source_version_id,
	'family_id',r.family_id,'guidance_ids',json(r.trigger_guidance_ids),'publication_key',r.publication_key,
	'target_document_id',r.target_document_id,'publish_error',r.publish_error) AS details
FROM ext_revision_proposals r LEFT JOIN ext_revision_split_parts m USING(tenant_id,knowledge_base_id,proposal_id)
UNION ALL
SELECT tenant_id,knowledge_base_id,batch_id,status,revision,
	json_object('record_kind','split_batch','source_document_id',source_document_id,'source_version_id',source_version_id,
	'first_proposal_id',COALESCE(first_proposal_id,'')) AS details
FROM ext_revision_split_batches
)`

func listSplitParts(ctx context.Context, tx *sql.Tx, scope provenance.Scope, id string) ([]map[string]any, error) {
	rows, err := tx.QueryContext(ctx, `SELECT r.proposal_id,p.title,r.proposed_patch,r.status,r.revision,r.target_document_id,r.publish_error
		FROM ext_revision_split_parts p JOIN ext_revision_proposals r USING(tenant_id,knowledge_base_id,proposal_id)
		WHERE p.tenant_id=? AND p.knowledge_base_id=? AND p.batch_id=? ORDER BY p.position`, args(scope, id)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	parts := []map[string]any{}
	for rows.Next() {
		var part, title, text, status, target, failure string
		var rev int64
		if err := rows.Scan(&part, &title, &text, &status, &rev, &target, &failure); err != nil {
			return nil, err
		}
		parts = append(parts, map[string]any{"id": part, "title": title, "proposed_patch": text,
			"status": status, "revision": rev, "target_document_id": target, "publish_error": failure})
	}
	return parts, rows.Err()
}
