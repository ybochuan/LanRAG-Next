package lineage

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/dependencies"
	"github.com/ybochuan/LanRAG-Next/extensions/parser"
	"github.com/ybochuan/LanRAG-Next/extensions/provenance"
	"github.com/ybochuan/LanRAG-Next/extensions/store"
)

// RenditionBinding records an immutable link from one durable conversion to a native version.
type RenditionBinding struct {
	ArtifactID string `json:"artifact_id"`
	DocumentID string `json:"document_id"`
	NodeID     string `json:"artifact_node_id"`
	VersionID  string `json:"source_version_id"`
	SourceHash string `json:"source_sha256"`
	ResultHash string `json:"artifact_sha256"`
	BoundAt    string `json:"bound_at"`
}

// Renditions joins the durable parser receipt with a scoped native original and snapshot.
type Renditions struct {
	registry     *Registry
	originalHash func(context.Context, string) (string, error)
}

// NewRenditions requires the native original downloader used by the scoped worker.
func NewRenditions(registry *Registry, originalHash func(context.Context, string) (string, error)) *Renditions {
	return &Renditions{registry: registry, originalHash: originalHash}
}

func (r *Renditions) validate(s provenance.Scope, doc string) error {
	if r == nil || r.registry == nil || r.registry.db == nil || r.originalHash == nil {
		return ErrUnavailable
	}
	if s.TenantID == 0 || !validID(s.KnowledgeBaseID) || !validID(doc) {
		return ErrEvidence
	}
	return nil
}

func readRenditionBinding(ctx context.Context, tx *sql.Tx, s provenance.Scope, doc, id string) (RenditionBinding, error) {
	b := RenditionBinding{ArtifactID: id, DocumentID: doc}
	err := tx.QueryRowContext(ctx, `SELECT artifact_node_id,source_version_id,source_sha256,artifact_sha256,bound_at
		FROM ext_parser_rendition_bindings WHERE tenant_id=? AND knowledge_base_id=? AND document_id=? AND identity_key=?`,
		scopeArgs(s, doc, id)...).Scan(&b.NodeID, &b.VersionID, &b.SourceHash, &b.ResultHash, &b.BoundAt)
	return b, err
}

func readRenditionArtifact(ctx context.Context, tx *sql.Tx, s provenance.Scope, doc, id string) (parser.RenditionArtifact, error) {
	a, err := parser.ReadRenditionTx(ctx, tx, s.TenantID, s.KnowledgeBaseID, doc, id)
	if errors.Is(err, parser.ErrRenditionEvidence) {
		return a, ErrEvidence
	}
	return a, err
}

type renditionObservation struct {
	versionID string
	parents   []dependencies.SourceRef
}

// verifyOriginal fences the original download with native snapshots. Downloads
// run outside the extension writer. Callers compare local heads in the final transaction.
func (r *Renditions) verifyOriginal(ctx context.Context, s provenance.Scope, doc, hash, pinned string) (renditionObservation, error) {
	before, err := r.registry.Refresh(ctx, s, doc)
	if err != nil {
		return renditionObservation{}, err
	}
	if pinned != "" && before.ID != pinned {
		return renditionObservation{}, ErrConflict
	}
	observation := renditionObservation{versionID: before.ID}
	err = r.registry.db.View(ctx, func(tx *sql.Tx) error {
		var err error
		observation.parents, err = renditionGovernance(ctx, tx, s, doc, before.ID)
		return err
	})
	if err != nil {
		return renditionObservation{}, err
	}
	for _, ref := range observation.parents {
		v := before
		if ref.DocumentID != doc {
			v, err = r.registry.Refresh(ctx, s, ref.DocumentID)
		}
		if err != nil || v.ID != ref.VersionID {
			return renditionObservation{}, ErrConflict
		}
	}
	actual, err := r.originalHash(ctx, doc)
	if err != nil {
		return renditionObservation{}, ErrUnavailable
	}
	if actual != hash {
		return renditionObservation{}, ErrConflict
	}
	after, err := r.registry.Refresh(ctx, s, doc)
	if err != nil {
		return renditionObservation{}, err
	}
	if after.ID != before.ID {
		return renditionObservation{}, ErrConflict
	}
	return observation, nil
}

func renditionCurrent(ctx context.Context, tx *sql.Tx, s provenance.Scope, doc string, observation renditionObservation) error {
	v, err := currentVersion(ctx, tx, s, doc)
	if err != nil || v.ID != observation.versionID {
		return ErrConflict
	}
	parents, err := renditionGovernance(ctx, tx, s, doc, observation.versionID)
	if err != nil || !slices.Equal(parents, observation.parents) {
		return ErrConflict
	}
	return nil
}

func renditionGovernance(ctx context.Context, tx *sql.Tx, s provenance.Scope, doc, version string) ([]dependencies.SourceRef, error) {
	var enabled string
	var from, until sql.NullString
	err := tx.QueryRowContext(ctx, `SELECT json_extract(evidence.snapshot_json,'$.header_fields.enable_status'),a.effective_from,a.effective_until
		FROM ext_lifecycle_current c `+store.LifecycleEventJoinSQL+`
		JOIN ext_source_snapshot_evidence evidence ON evidence.tenant_id=c.tenant_id AND evidence.knowledge_base_id=c.knowledge_base_id
		AND evidence.version_node_id=?
		LEFT JOIN ext_document_annotations a ON a.tenant_id=c.tenant_id AND a.knowledge_base_id=c.knowledge_base_id AND a.document_id=c.document_id
		WHERE c.tenant_id=? AND c.knowledge_base_id=? AND c.document_id=? AND c.state='active'
		AND `+store.LifecycleReadableSQL+` AND NOT `+store.SplitTargetPendingSQL,
		append([]any{version}, scopeArgs(s, doc)...)...).Scan(&enabled, &from, &until)
	if err != nil || enabled != sha([]byte(`"enabled"`)) {
		return nil, ErrConflict
	}
	now := time.Now().UTC()
	if from.Valid {
		date, err := time.Parse(time.RFC3339Nano, from.String)
		if err != nil || now.Before(date) {
			return nil, ErrConflict
		}
	}
	if until.Valid {
		date, err := time.Parse(time.RFC3339Nano, until.String)
		if err != nil || now.After(date) {
			return nil, ErrConflict
		}
	}
	assessment, err := dependencies.LoadTx(ctx, tx, dependencies.Scope{TenantID: s.TenantID, KnowledgeBaseID: s.KnowledgeBaseID, DocumentID: doc})
	if err != nil || assessment.ReadBlocked(now) {
		return nil, ErrConflict
	}
	return assessment.Sources, nil
}

// Bind certifies an existing result. An empty ID selects the only result for doc.
// It never creates a parse job or a native document.
func (r *Renditions) Bind(ctx context.Context, s provenance.Scope, doc, id string) (RenditionBinding, error) {
	if err := r.validate(s, doc); err != nil {
		return RenditionBinding{}, err
	}
	if id != "" && !validHash(id) {
		return RenditionBinding{}, ErrEvidence
	}
	var candidate parser.RenditionArtifact
	var pinned string
	err := r.registry.db.View(ctx, func(tx *sql.Tx) error {
		if id == "" {
			var count int
			err := tx.QueryRowContext(ctx, `SELECT COUNT(*),coalesce(min(identity_key),'') FROM (
				SELECT identity_key FROM ext_parser_artifacts WHERE tenant_id=? AND knowledge_base_id=? AND doc_id=? LIMIT 2
			)`, s.TenantID, s.KnowledgeBaseID, doc).Scan(&count, &id)
			if err != nil {
				return err
			}
			if count > 1 {
				return ErrConflict
			}
			if count != 1 || !validHash(id) {
				return ErrEvidence
			}
		}
		var err error
		candidate, err = readRenditionArtifact(ctx, tx, s, doc, id)
		if err != nil {
			return err
		}
		b, err := readRenditionBinding(ctx, tx, s, doc, id)
		pinned = b.VersionID
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return err
	})
	if err != nil {
		return RenditionBinding{}, err
	}
	observation, err := r.verifyOriginal(ctx, s, doc, candidate.SourceSHA256, pinned)
	if err != nil {
		return RenditionBinding{}, err
	}
	var result RenditionBinding
	err = r.registry.db.Write(ctx, func(tx *sql.Tx) error {
		if err := renditionCurrent(ctx, tx, s, doc, observation); err != nil {
			return err
		}
		a, err := readRenditionArtifact(ctx, tx, s, doc, id)
		if err != nil {
			return err
		}
		if a.SHA256 != candidate.SHA256 || a.SourceSHA256 != candidate.SourceSHA256 {
			return ErrConflict
		}
		result, err = readRenditionBinding(ctx, tx, s, doc, id)
		if err == nil {
			if result.VersionID != observation.versionID || result.SourceHash != a.SourceSHA256 || result.ResultHash != a.SHA256 {
				return ErrConflict
			}
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		node, err := newID()
		if err != nil {
			return err
		}
		now := time.Now().UTC().Format(time.RFC3339Nano)
		if _, err := tx.ExecContext(ctx, `INSERT INTO ext_lineage_node VALUES(?,?,?,?,?,?)`,
			append([]any{node}, scopeArgs(s, "parser_artifact", id, now)...)...); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO ext_parser_rendition_bindings(identity_key,tenant_id,knowledge_base_id,document_id,
			artifact_node_id,source_version_id,source_sha256,artifact_sha256,bound_at) VALUES(?,?,?,?,?,?,?,?,?)`,
			append([]any{id}, scopeArgs(s, doc, node, observation.versionID, a.SourceSHA256, a.SHA256, now)...)...)
		result = RenditionBinding{ArtifactID: id, DocumentID: doc, NodeID: node, VersionID: observation.versionID, SourceHash: a.SourceSHA256, ResultHash: a.SHA256, BoundAt: now}
		return err
	})
	if err != nil {
		return RenditionBinding{}, err
	}
	return result, nil
}

// Read rechecks the pinned version and source bytes before returning unchanged conversion bytes.
func (r *Renditions) Read(ctx context.Context, s provenance.Scope, doc, id string) (RenditionBinding, []byte, error) {
	if err := r.validate(s, doc); err != nil {
		return RenditionBinding{}, nil, err
	}
	if !validHash(id) {
		return RenditionBinding{}, nil, ErrEvidence
	}
	var b RenditionBinding
	err := r.registry.db.View(ctx, func(tx *sql.Tx) error {
		var err error
		b, err = readRenditionBinding(ctx, tx, s, doc, id)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrEvidence
		}
		return err
	})
	if err != nil {
		return RenditionBinding{}, nil, err
	}
	observation, err := r.verifyOriginal(ctx, s, doc, b.SourceHash, b.VersionID)
	if err != nil {
		return RenditionBinding{}, nil, err
	}
	var body []byte
	err = r.registry.db.View(ctx, func(tx *sql.Tx) error {
		if err := renditionCurrent(ctx, tx, s, doc, observation); err != nil {
			return err
		}
		a, err := readRenditionArtifact(ctx, tx, s, doc, id)
		if err != nil {
			return err
		}
		if a.SHA256 != b.ResultHash || a.SourceSHA256 != b.SourceHash {
			return ErrConflict
		}
		body = a.Body
		return nil
	})
	if err != nil {
		return RenditionBinding{}, nil, err
	}
	return b, body, nil
}
