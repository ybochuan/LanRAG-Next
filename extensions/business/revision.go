package business

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/ybochuan/LanRAG-Next/extensions/lifecycle"
	"github.com/ybochuan/LanRAG-Next/extensions/outbox"
	"github.com/ybochuan/LanRAG-Next/extensions/provenance"
	"strings"
	"time"
)

func textHash(text string) string { h := sha256.Sum256([]byte(text)); return hex.EncodeToString(h[:]) }

const publicationTime = time.RFC3339Nano

// Publication is immutable approved full Markdown; it never overwrites source bytes.
type Publication struct {
	Scope              provenance.Scope `json:"scope"`
	ProposalID         string           `json:"proposal_id"`
	SourceDocumentID   string           `json:"source_document_id"`
	SourceVersionID    string           `json:"source_version_id"`
	Content            string           `json:"content"`
	ContentHash        string           `json:"content_hash"`
	Key                string           `json:"publication_key"`
	TargetDocumentID   string           `json:"target_document_id,omitempty"`
	FamilyID           string           `json:"family_id"`
	SplitID            string           `json:"-"`
	SplitTitle         string           `json:"-"`
	TargetMetadataHash string           `json:"-"`
}

// RemoteDocument reports a positively reconciled native document.
type RemoteDocument struct {
	ID           string
	ContentHash  string
	Indexed      bool
	Draft        bool
	IndexFailed  bool
	MetadataHash string
}

// Remote distinguishes authoritative absence (empty ID,nil) from an unknown result (error).
type Remote interface {
	Reconcile(context.Context, Publication) (RemoteDocument, error)
	Create(context.Context, Publication) (RemoteDocument, error)
}

// WithRemote configures the native sidecar publisher, leaving the original unchanged.
func (s *Service) WithRemote(remote Remote) *Service { s.remote = remote; return s }

func (s *Service) revision(ctx context.Context, c Command) (Result, error) {
	var source string
	var e error
	if c.Action == "revision_create" {
		if !validID(c.Payload.DocumentID) || len(c.Payload.GuidanceIDs) == 0 || len(c.Payload.GuidanceIDs) > 3 || strings.TrimSpace(c.Payload.Text) == "" || len(c.Payload.Text) > 48000 {
			return Result{}, ErrInvalid
		}
		source, e = s.verify(ctx, c.Scope, c.Payload.DocumentID)
		if e != nil {
			return Result{}, e
		}
	}
	return s.transact(ctx, c, func(tx *sql.Tx) (Result, error) {
		out := Result{ID: c.ID, Revision: c.ExpectedRevision + 1}
		if c.Action == "revision_create" {
			if c.ExpectedRevision != 0 {
				return Result{}, ErrConflict
			}
			if e = s.current(ctx, tx, c.Scope, c.Payload.DocumentID, source); e != nil {
				return Result{}, e
			}
			if e = requireRevisionGuidances(ctx, tx, c.Scope, c.Payload.GuidanceIDs); e != nil {
				return Result{}, e
			}
			out.ID = newID()
			if out.ID == "" {
				return Result{}, ErrUnavailable
			}
			out.Status = "pending_review"
			content := strings.TrimSpace(strings.ReplaceAll(c.Payload.Text, "\r\n", "\n")) + "\n"
			hash := textHash(content)
			out.PublicationKey = textHash(out.ID + source + hash)
			guidance, _ := json.Marshal(c.Payload.GuidanceIDs)
			var family string
			if e = tx.QueryRowContext(ctx, `SELECT COALESCE((SELECT family_id FROM ext_revision_proposals WHERE tenant_id=f.tenant_id AND knowledge_base_id=f.knowledge_base_id AND target_document_id=f.document_id AND status IN ('published','superseded') ORDER BY published_at DESC LIMIT 1),f.family_id) FROM ext_document_version_family f WHERE f.tenant_id=? AND f.knowledge_base_id=? AND f.document_id=?`, args(c.Scope, c.Payload.DocumentID)...).Scan(&family); e != nil {
				return Result{}, e
			}
			_, e = tx.ExecContext(ctx, `INSERT INTO ext_revision_proposals(tenant_id,knowledge_base_id,proposal_id,source_document_id,source_version_id,family_id,trigger_guidance_ids,proposed_patch,target_content_hash,status,revision,publication_key,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,1,?,?)`, args(c.Scope, out.ID, c.Payload.DocumentID, source, family, string(guidance), content, hash, out.Status, out.PublicationKey, s.now().UTC().Format(time.RFC3339Nano))...)
			return out, e
		}
		var status string
		var revision int64
		if e = tx.QueryRowContext(ctx, `SELECT r.status,r.revision,r.publication_key FROM ext_revision_proposals r
			WHERE r.tenant_id=? AND r.knowledge_base_id=? AND r.proposal_id=? AND NOT EXISTS(
				SELECT 1 FROM ext_revision_split_parts p WHERE p.tenant_id=r.tenant_id
				AND p.knowledge_base_id=r.knowledge_base_id AND p.proposal_id=r.proposal_id)`,
			args(c.Scope, c.ID)...).Scan(&status, &revision, &out.PublicationKey); e != nil || revision != c.ExpectedRevision || status != "pending_review" {
			return Result{}, ErrConflict
		}
		out.Status = "approved"
		if c.Action == "revision_reject" {
			out.Status = "rejected"
		}
		_, e = tx.ExecContext(ctx, `UPDATE ext_revision_proposals SET status=?,revision=? WHERE tenant_id=? AND knowledge_base_id=? AND proposal_id=?`, append([]any{out.Status, out.Revision}, args(c.Scope, c.ID)...)...)
		return out, e
	})
}
func requireRevisionGuidances(ctx context.Context, tx *sql.Tx, scope provenance.Scope, ids []string) error {
	seen := map[string]bool{}
	for _, id := range ids {
		if !validID(id) || seen[id] {
			return ErrInvalid
		}
		seen[id] = true
		var one int
		if err := tx.QueryRowContext(ctx, `SELECT 1 FROM ext_feedback_guidances
			WHERE tenant_id=? AND knowledge_base_id=? AND guidance_id=? AND status='active'`, args(scope, id)...).Scan(&one); err != nil {
			return ErrConflict
		}
	}
	return nil
}
func (s *Service) publication(ctx context.Context, scope provenance.Scope, id string) (p Publication, r Result, lease sql.NullString, err error) {
	p.Scope = scope
	p.ProposalID = id
	r.ID = id
	err = s.db.View(ctx, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `SELECT r.source_document_id,r.source_version_id,r.proposed_patch,r.target_content_hash,
			r.publication_key,r.target_document_id,r.status,r.revision,r.publish_lease_until,r.family_id,
			COALESCE(m.batch_id,''),COALESCE(m.title,'') FROM ext_revision_proposals r
			LEFT JOIN ext_revision_split_parts m USING(tenant_id,knowledge_base_id,proposal_id)
			WHERE r.tenant_id=? AND r.knowledge_base_id=? AND r.proposal_id=?`,
			args(scope, id)...).Scan(&p.SourceDocumentID, &p.SourceVersionID, &p.Content, &p.ContentHash, &p.Key, &p.TargetDocumentID,
			&r.Status, &r.Revision, &lease, &p.FamilyID, &p.SplitID, &p.SplitTitle)
	})
	r.PublicationKey = p.Key
	r.TargetDocumentID = p.TargetDocumentID
	return
}

// Publish persists intent, serializes attempts, then reconciles BEFORE any create.
// A remote error ends this attempt; retry only the Saga, never blindly replay Create.
func (s *Service) Publish(ctx context.Context, scope provenance.Scope, id string) (Result, error) {
	if s == nil || s.db == nil || s.remote == nil {
		return Result{}, ErrUnavailable
	}
	if !validID(id) {
		return Result{}, ErrInvalid
	}
	p, r, lease, e := s.publication(ctx, scope, id)
	if e != nil {
		return Result{}, ErrConflict
	}
	if r.Status == "published" || r.Status == "superseded" {
		return r, nil
	}
	if r.Status != "approved" && r.Status != "publishing" && r.Status != "publish_failed" && r.Status != "indexed" {
		return Result{}, ErrConflict
	}
	if lease.Valid {
		until, e := time.Parse(time.RFC3339Nano, lease.String)
		if e != nil || s.now().Before(until) {
			return Result{}, ErrConflict
		}
	}
	verifySource := s.verify
	if p.SplitID != "" {
		verifySource = s.observeSplitVersion
	}
	source, e := verifySource(ctx, scope, p.SourceDocumentID)
	if e != nil || source != p.SourceVersionID {
		return Result{}, ErrConflict
	}
	c := Command{Scope: scope, ActorID: "system:publisher", OperationID: "publish-claim-" + newID(), Action: "revision_publish_claim", ID: id, ExpectedRevision: r.Revision}
	r, e = s.transact(ctx, c, func(tx *sql.Tx) (Result, error) {
		var rev int64
		var l sql.NullString
		if e := tx.QueryRowContext(ctx, `SELECT revision,publish_lease_until FROM ext_revision_proposals WHERE tenant_id=? AND knowledge_base_id=? AND proposal_id=?`, args(scope, id)...).Scan(&rev, &l); e != nil || rev != r.Revision {
			return Result{}, ErrConflict
		}
		if _, e := s.publicationSource(ctx, tx, p); e != nil {
			return Result{}, e
		}
		out := Result{ID: id, Revision: rev + 1, Status: "publishing", PublicationKey: p.Key, TargetDocumentID: p.TargetDocumentID}
		_, e := tx.ExecContext(ctx, `UPDATE ext_revision_proposals SET status='publishing',revision=?,publish_lease_until=? WHERE tenant_id=? AND knowledge_base_id=? AND proposal_id=?`, append([]any{out.Revision, s.now().Add(2 * time.Minute).UTC().Format(time.RFC3339Nano)}, args(scope, id)...)...)
		return out, e
	})
	if e != nil {
		return Result{}, e
	}
	remoteCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if p.SplitID != "" {
		return s.publishSplit(ctx, remoteCtx, p, r)
	}
	target, remoteErr := s.remote.Reconcile(remoteCtx, p)
	if remoteErr == nil && target.ID == "" {
		if p.TargetDocumentID != "" {
			remoteErr = ErrConflict
		} else {
			target, remoteErr = s.remote.Create(remoteCtx, p)
		}
	}
	if remoteErr != nil {
		return s.publishFailure(ctx, p, r, remoteErr)
	}
	if !validID(target.ID) || target.ContentHash != p.ContentHash {
		return s.publishMismatch(ctx, p, r)
	}
	status := "publishing"
	if target.Indexed {
		status = "indexed"
	}
	r, e = s.publishState(ctx, p, r, status, "", target.ID, "")
	if e != nil {
		return Result{}, e
	}
	if !target.Indexed {
		return r, nil
	}
	targetVersion, e := s.verify(ctx, scope, target.ID)
	if e != nil {
		return r, nil
	} // Observer may not yet have initialized target lifecycle.
	return s.publishState(ctx, p, r, "published", "", target.ID, targetVersion)
}

func (s *Service) publishFailure(ctx context.Context, p Publication, r Result, cause error) (Result, error) {
	code := "remote_result_unknown"
	if errors.Is(cause, ErrBinarySource) {
		code = "binary_source_upload_required"
	}
	_, _ = s.publishState(context.WithoutCancel(ctx), p, r, "publish_failed", code, "", "")
	return Result{}, ErrUnavailable
}

func (s *Service) publishMismatch(ctx context.Context, p Publication, r Result) (Result, error) {
	_, _ = s.publishState(context.WithoutCancel(ctx), p, r, "publish_failed", "remote_identity_mismatch", "", "")
	return Result{}, ErrConflict
}

func (s *Service) retireRevisionSource(ctx context.Context, tx *sql.Tx, p Publication, eventID, reason string, now time.Time) error {
	if _, err := lifecycle.AppendTx(ctx, tx, lifecycle.Event{
		EventID: eventID, TenantID: p.Scope.TenantID, KnowledgeBaseID: p.Scope.KnowledgeBaseID,
		DocumentID: p.SourceDocumentID, FromState: lifecycle.Active, ToState: "superseded", Reason: reason, OccurredAt: now,
	}); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `UPDATE ext_revision_proposals SET status='superseded',revision=revision+1
		WHERE tenant_id=? AND knowledge_base_id=? AND target_document_id=? AND status='published'`, args(p.Scope, p.SourceDocumentID)...)
	return err
}

func (s *Service) publishState(ctx context.Context, p Publication, previous Result, status, code, target, version string) (Result, error) {
	c := Command{Scope: p.Scope, ActorID: "system:publisher", OperationID: "publish-state-" + newID(), Action: "revision_" + status, ID: p.ProposalID, ExpectedRevision: previous.Revision}
	return s.transact(ctx, c, func(tx *sql.Tx) (Result, error) {
		var rev int64
		if e := tx.QueryRowContext(ctx, `SELECT revision FROM ext_revision_proposals WHERE tenant_id=? AND knowledge_base_id=? AND proposal_id=?`, args(p.Scope, p.ProposalID)...).Scan(&rev); e != nil || rev != previous.Revision {
			return Result{}, ErrConflict
		}
		out := Result{ID: p.ProposalID, Revision: rev + 1, Status: status, PublicationKey: p.Key, TargetDocumentID: target}
		if target == "" {
			out.TargetDocumentID = previous.TargetDocumentID
		}
		var published any
		family := p.FamilyID
		if status == "published" {
			first, e := s.publicationSource(ctx, tx, p)
			if e != nil {
				return Result{}, e
			}
			if e := s.current(ctx, tx, p.Scope, target, version); e != nil {
				return Result{}, e
			}
			published = s.now().UTC().Format(time.RFC3339Nano)
			if p.SplitID != "" {
				family, e = s.finalizeSplit(ctx, tx, p, target, version, first)
			} else {
				e = s.finalizeRevision(ctx, tx, p, target, version)
			}
			if e != nil {
				return Result{}, e
			}
		}
		retainLease := p.SplitID != "" && code == "remote_result_unknown"
		_, e := tx.ExecContext(ctx, `UPDATE ext_revision_proposals SET status=?,revision=?,target_document_id=?,publish_error=?,
			published_at=COALESCE(?,published_at),family_id=?,publish_lease_until=CASE WHEN ? THEN publish_lease_until ELSE NULL END
			WHERE tenant_id=? AND knowledge_base_id=? AND proposal_id=?`,
			append([]any{out.Status, out.Revision, out.TargetDocumentID, code, published, family, retainLease}, args(p.Scope, p.ProposalID)...)...)
		return out, e
	})
}

func (s *Service) finalizeRevision(ctx context.Context, tx *sql.Tx, p Publication, target, version string) error {
	now := s.now().UTC()
	edge := textHash(p.Key + "supersedes")
	if _, err := tx.ExecContext(ctx, `INSERT INTO ext_lineage_edge VALUES(?,?,?,?,?,'supersedes','governance_critical',?) ON CONFLICT DO NOTHING`,
		append([]any{edge}, args(p.Scope, version, p.SourceVersionID, now.Format(publicationTime))...)...); err != nil {
		return err
	}
	if err := s.retireRevisionSource(ctx, tx, p, "revision-"+p.Key, "revision_published", now); err != nil {
		return err
	}
	for _, doc := range []string{p.SourceDocumentID, target} {
		payload, _ := json.Marshal(map[string]string{"document_id": doc, "family_id": p.FamilyID})
		if err := outbox.EnqueueTx(ctx, tx, outbox.Item{ID: "family:" + p.Key + ":" + doc, TenantID: p.Scope.TenantID,
			KnowledgeBaseID: p.Scope.KnowledgeBaseID, ProjectionType: "family_tag", AggregateType: "document", AggregateID: doc,
			CausationType: "document_version", CausationID: version, Payload: payload, CreatedAt: now}); err != nil {
			return err
		}
	}
	return nil
}
