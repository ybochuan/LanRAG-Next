package business

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"strings"

	"github.com/ybochuan/LanRAG-Next/extensions/dependencies"
	"github.com/ybochuan/LanRAG-Next/extensions/provenance"
	"github.com/ybochuan/LanRAG-Next/extensions/store"
)

// ClauseAnchor identifies the exact native chunk selected for review.
type ClauseAnchor struct {
	ChunkID     string `json:"chunk_id"`
	ContentHash string `json:"content_hash"`
}

func (a *ClauseAnchor) valid() bool {
	if a == nil || !validID(a.ChunkID) || len(a.ContentHash) != 64 || a.ContentHash != strings.ToLower(a.ContentHash) {
		return false
	}
	_, err := hex.DecodeString(a.ContentHash)
	return err == nil && a.ContentHash != textHash("")
}

type clausePin struct {
	DocumentID, VersionID string
	ClauseAnchor
}

type clauseRelation struct {
	source, target clausePin
	kind, status   string
	revision       int64
}

func readClause(ctx context.Context, tx *sql.Tx, scope provenance.Scope, id string) (clauseRelation, error) {
	var r clauseRelation
	err := tx.QueryRowContext(ctx, `SELECT document_id,source_version_id,source_chunk_id,source_content_hash,
		target_document_id,target_version_id,target_chunk_id,target_content_hash,edge_type,status,revision
		FROM ext_clause_relations WHERE tenant_id=? AND knowledge_base_id=? AND relation_id=?`, args(scope, id)...).
		Scan(&r.source.DocumentID, &r.source.VersionID, &r.source.ChunkID, &r.source.ContentHash,
			&r.target.DocumentID, &r.target.VersionID, &r.target.ChunkID, &r.target.ContentHash, &r.kind, &r.status, &r.revision)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrConflict
	}
	return r, err
}

func (s *Service) checkClause(ctx context.Context, tx *sql.Tx, scope provenance.Scope, r clauseRelation) error {
	for _, pin := range []clausePin{r.source, r.target} {
		if err := s.currentEnabled(ctx, tx, scope, pin.DocumentID, pin.VersionID); err != nil {
			return err
		}
		a, err := dependencies.LoadTx(ctx, tx, dependencies.Scope{TenantID: scope.TenantID, KnowledgeBaseID: scope.KnowledgeBaseID, DocumentID: pin.DocumentID})
		if err != nil || a.ReadBlocked(s.now()) {
			return ErrConflict
		}
		var found bool
		err = tx.QueryRowContext(ctx, `SELECT EXISTS(
			SELECT 1 FROM ext_source_snapshot_evidence e, json_each(e.snapshot_json,'$.chunks') c
			WHERE e.tenant_id=? AND e.knowledge_base_id=? AND e.version_node_id=?
			AND json_extract(c.value,'$.id')=? AND json_extract(c.value,'$.content_hash')=?)
			AND EXISTS(SELECT 1 FROM ext_lifecycle_current c WHERE c.tenant_id=? AND c.knowledge_base_id=?
			AND c.document_id=? AND NOT `+store.RepealDueSQL+` AND NOT `+store.SplitTargetPendingSQL+`)`,
			append(args(scope, pin.VersionID, pin.ChunkID, pin.ContentHash), args(scope, pin.DocumentID)...)...).Scan(&found)
		if err != nil {
			return err
		}
		if !found {
			return ErrConflict
		}
	}
	return nil
}

func (s *Service) clause(ctx context.Context, c Command) (Result, error) {
	if s == nil || s.db == nil {
		return Result{}, ErrUnavailable
	}
	if c.Scope.TenantID == 0 || !validID(c.Scope.KnowledgeBaseID) || !validID(c.ActorID) || !validID(c.OperationID) || c.ExpectedRevision < 0 {
		return Result{}, ErrInvalid
	}
	if result, replayed, err := s.replay(ctx, c); replayed || err != nil {
		return result, err
	}
	if c.Action == "clause_create" {
		return s.createClause(ctx, c)
	}
	if !validID(c.ID) {
		return Result{}, ErrInvalid
	}
	note := strings.TrimSpace(c.Payload.Text)
	if c.Action == "clause_resolve" {
		switch c.Payload.Resolution {
		case "prefer_source", "prefer_target", "contextual":
		default:
			return Result{}, ErrInvalid
		}
		if note == "" || len(note) > 4000 {
			return Result{}, ErrInvalid
		}
	}
	if c.Action != "clause_revoke" {
		var observed clauseRelation
		err := s.db.View(ctx, func(tx *sql.Tx) error {
			var err error
			observed, err = readClause(ctx, tx, c.Scope, c.ID)
			return err
		})
		if err != nil || observed.revision != c.ExpectedRevision {
			if err == nil {
				err = ErrConflict
			}
			return s.replayAfterObservationError(ctx, c, err)
		}
		for _, pin := range []clausePin{observed.source, observed.target} {
			version, err := s.verify(ctx, c.Scope, pin.DocumentID)
			if err != nil {
				return s.replayAfterObservationError(ctx, c, err)
			}
			if version != pin.VersionID {
				return s.replayAfterObservationError(ctx, c, ErrConflict)
			}
		}
	}
	return s.transact(ctx, c, func(tx *sql.Tx) (Result, error) {
		r, err := readClause(ctx, tx, c.Scope, c.ID)
		if err != nil {
			return Result{}, err
		}
		if r.revision != c.ExpectedRevision || r.status == "revoked" {
			return Result{}, ErrConflict
		}
		status := "revoked"
		if c.Action != "clause_revoke" {
			if err := s.checkClause(ctx, tx, c.Scope, r); err != nil {
				return Result{}, err
			}
			if c.Action == "clause_confirm" {
				if r.status != "pending_review" {
					return Result{}, ErrConflict
				}
				status = "active"
				if r.kind == "conflicts_with" {
					status = "awaiting_resolution"
				}
			} else {
				if r.status != "awaiting_resolution" || r.kind != "conflicts_with" {
					return Result{}, ErrConflict
				}
				status = "resolved"
				if _, err := tx.ExecContext(ctx, `UPDATE ext_clause_relations
					SET resolution=?,decision_note=?,resolved_by=?,resolved_at=?
					WHERE tenant_id=? AND knowledge_base_id=? AND relation_id=?`,
					append([]any{c.Payload.Resolution, note, c.ActorID, s.now().UTC().Format(publicationTime)}, args(c.Scope, c.ID)...)...); err != nil {
					return Result{}, err
				}
			}
		}
		_, err = tx.ExecContext(ctx, `UPDATE ext_clause_relations SET status=?,revision=revision+1
			WHERE tenant_id=? AND knowledge_base_id=? AND relation_id=?`, append([]any{status}, args(c.Scope, c.ID)...)...)
		return Result{ID: c.ID, Status: status, Revision: r.revision + 1}, err
	})
}

func (s *Service) createClause(ctx context.Context, c Command) (Result, error) {
	p := c.Payload
	conditions := strings.TrimSpace(p.AppliesWhen)
	if c.ExpectedRevision != 0 || !validID(p.DocumentID) || !validID(p.TargetDocumentID) || p.DocumentID == p.TargetDocumentID ||
		(p.EdgeType != "exception_to" && p.EdgeType != "conflicts_with") || !p.SourceClause.valid() || !p.TargetClause.valid() ||
		conditions == "" || len(conditions) > 4000 {
		return Result{}, ErrInvalid
	}
	source, err := s.verify(ctx, c.Scope, p.DocumentID)
	if err != nil {
		return s.replayAfterObservationError(ctx, c, err)
	}
	target, err := s.verify(ctx, c.Scope, p.TargetDocumentID)
	if err != nil {
		return s.replayAfterObservationError(ctx, c, err)
	}
	r := clauseRelation{source: clausePin{p.DocumentID, source, *p.SourceClause},
		target: clausePin{p.TargetDocumentID, target, *p.TargetClause}}
	pair := [2]clausePin{r.source, r.target}
	if p.EdgeType == "conflicts_with" && pair[0].DocumentID > pair[1].DocumentID {
		pair[0], pair[1] = pair[1], pair[0]
	}
	return s.transact(ctx, c, func(tx *sql.Tx) (Result, error) {
		if err := s.checkClause(ctx, tx, c.Scope, r); err != nil {
			return Result{}, err
		}
		out := Result{ID: newID(), Status: "pending_review", Revision: 1}
		if out.ID == "" {
			return Result{}, ErrUnavailable
		}
		insert, err := tx.ExecContext(ctx, `INSERT INTO ext_clause_relations(tenant_id,knowledge_base_id,relation_id,document_id,
			target_document_id,source_version_id,target_version_id,source_chunk_id,target_chunk_id,source_content_hash,target_content_hash,
			edge_type,pair_key,applies_when,status,revision,created_at)
			VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,'pending_review',1,?) ON CONFLICT DO NOTHING`,
			args(c.Scope, out.ID, p.DocumentID, p.TargetDocumentID, source, target, p.SourceClause.ChunkID, p.TargetClause.ChunkID,
				p.SourceClause.ContentHash, p.TargetClause.ContentHash, p.EdgeType, digest(pair), conditions, s.now().UTC().Format(publicationTime))...)
		if err != nil {
			return Result{}, err
		}
		count, err := insert.RowsAffected()
		if err == nil && count != 1 {
			return Result{}, ErrConflict
		}
		return out, err
	})
}
