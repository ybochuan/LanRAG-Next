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

// Publication is immutable approved full Markdown; it never overwrites source bytes.
type Publication struct {
	Scope            provenance.Scope `json:"scope"`
	ProposalID       string           `json:"proposal_id"`
	SourceDocumentID string           `json:"source_document_id"`
	SourceVersionID  string           `json:"source_version_id"`
	Content          string           `json:"content"`
	ContentHash      string           `json:"content_hash"`
	Key              string           `json:"publication_key"`
	TargetDocumentID string           `json:"target_document_id,omitempty"`
	FamilyID         string           `json:"family_id"`
}

// RemoteDocument reports a positively reconciled native document.
type RemoteDocument struct {
	ID          string
	ContentHash string
	Indexed     bool
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
			seen := map[string]bool{}
			for _, g := range c.Payload.GuidanceIDs {
				if !validID(g) || seen[g] {
					return Result{}, ErrInvalid
				}
				seen[g] = true
				var one int
				if e = tx.QueryRowContext(ctx, `SELECT 1 FROM ext_feedback_guidances WHERE tenant_id=? AND knowledge_base_id=? AND guidance_id=? AND status='active'`, args(c.Scope, g)...).Scan(&one); e != nil {
					return Result{}, ErrConflict
				}
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
		if e = tx.QueryRowContext(ctx, `SELECT status,revision,publication_key FROM ext_revision_proposals WHERE tenant_id=? AND knowledge_base_id=? AND proposal_id=?`, args(c.Scope, c.ID)...).Scan(&status, &revision, &out.PublicationKey); e != nil || revision != c.ExpectedRevision || status != "pending_review" {
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
func (s *Service) publication(ctx context.Context, scope provenance.Scope, id string) (p Publication, r Result, lease sql.NullString, err error) {
	p.Scope = scope
	p.ProposalID = id
	r.ID = id
	err = s.db.View(ctx, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `SELECT source_document_id,source_version_id,proposed_patch,target_content_hash,publication_key,target_document_id,status,revision,publish_lease_until,family_id FROM ext_revision_proposals WHERE tenant_id=? AND knowledge_base_id=? AND proposal_id=?`, args(scope, id)...).Scan(&p.SourceDocumentID, &p.SourceVersionID, &p.Content, &p.ContentHash, &p.Key, &p.TargetDocumentID, &r.Status, &r.Revision, &lease, &p.FamilyID)
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
	source, e := s.verify(ctx, scope, p.SourceDocumentID)
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
		if e := s.current(ctx, tx, scope, p.SourceDocumentID, source); e != nil {
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
	target, remoteErr := s.remote.Reconcile(remoteCtx, p)
	if remoteErr == nil && target.ID == "" {
		if p.TargetDocumentID != "" {
			remoteErr = ErrConflict
		} else {
			target, remoteErr = s.remote.Create(remoteCtx, p)
		}
	}
	if remoteErr != nil {
		code := "remote_result_unknown"
		if errors.Is(remoteErr, ErrBinarySource) {
			code = "binary_source_upload_required"
		}
		_, _ = s.publishState(context.WithoutCancel(ctx), p, r, "publish_failed", code, "", "")
		return Result{}, ErrUnavailable
	}
	if !validID(target.ID) || target.ContentHash != p.ContentHash {
		_, _ = s.publishState(context.WithoutCancel(ctx), p, r, "publish_failed", "remote_identity_mismatch", "", "")
		return Result{}, ErrConflict
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
		if status == "published" {
			if e := s.current(ctx, tx, p.Scope, p.SourceDocumentID, p.SourceVersionID); e != nil {
				return Result{}, e
			}
			if e := s.current(ctx, tx, p.Scope, target, version); e != nil {
				return Result{}, e
			}
			edge := textHash(p.Key + "supersedes")
			if _, e := tx.ExecContext(ctx, `INSERT INTO ext_lineage_edge VALUES(?,?,?,?,?,'supersedes','governance_critical',?) ON CONFLICT DO NOTHING`, append([]any{edge}, args(p.Scope, version, p.SourceVersionID, s.now().UTC().Format(time.RFC3339Nano))...)...); e != nil {
				return Result{}, e
			}
			published = s.now().UTC().Format(time.RFC3339Nano)
			if _, e := lifecycle.AppendTx(ctx, tx, lifecycle.Event{EventID: "revision-" + p.Key, TenantID: p.Scope.TenantID, KnowledgeBaseID: p.Scope.KnowledgeBaseID, DocumentID: p.SourceDocumentID, FromState: lifecycle.Active, ToState: "superseded", Reason: "revision_published", OccurredAt: s.now().UTC()}); e != nil {
				return Result{}, e
			}
			if _, e := tx.ExecContext(ctx, `UPDATE ext_revision_proposals SET status='superseded',revision=revision+1 WHERE tenant_id=? AND knowledge_base_id=? AND target_document_id=? AND status='published'`, args(p.Scope, p.SourceDocumentID)...); e != nil {
				return Result{}, e
			}
			for _, doc := range []string{p.SourceDocumentID, target} {
				payload, _ := json.Marshal(map[string]string{"document_id": doc, "family_id": p.FamilyID})
				if e := outbox.EnqueueTx(ctx, tx, outbox.Item{ID: "family:" + p.Key + ":" + doc, TenantID: p.Scope.TenantID, KnowledgeBaseID: p.Scope.KnowledgeBaseID, ProjectionType: "family_tag", AggregateType: "document", AggregateID: doc, CausationType: "document_version", CausationID: version, Payload: payload, CreatedAt: s.now().UTC()}); e != nil {
					return Result{}, e
				}
			}
		}
		_, e := tx.ExecContext(ctx, `UPDATE ext_revision_proposals SET status=?,revision=?,target_document_id=?,publish_error=?,published_at=COALESCE(?,published_at),publish_lease_until=NULL WHERE tenant_id=? AND knowledge_base_id=? AND proposal_id=?`, append([]any{out.Status, out.Revision, out.TargetDocumentID, code, published}, args(p.Scope, p.ProposalID)...)...)
		return out, e
	})
}
