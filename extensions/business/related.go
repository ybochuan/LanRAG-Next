package business

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/lineage"
	"github.com/ybochuan/LanRAG-Next/extensions/outbox"
	"github.com/ybochuan/LanRAG-Next/extensions/provenance"
	"github.com/ybochuan/LanRAG-Next/extensions/retrieval"
	"github.com/ybochuan/LanRAG-Next/extensions/store"
	"github.com/ybochuan/LanRAG-Next/extensions/taggov"
)

// Related is a currently active relationship without document content. It is
// scoped to the knowledge base of the related document, which may differ from
// the source document's knowledge base for normative relations.
type Related struct {
	KnowledgeBaseID string                     `json:"knowledge_base_id"`
	DocumentID      string                     `json:"document_id"`
	EdgeType        string                     `json:"edge_type"`
	Confidence      float64                    `json:"confidence"`
	Direction       string                     `json:"direction"`
	RelationID      string                     `json:"-"`
	Revision        int64                      `json:"-"`
	Reference       *store.AttachmentReference `json:"-"`
}

func score(p Payload) (float64, error) {
	for _, v := range []float64{p.Similarity, p.Declaration, p.Structure} {
		if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 || v > 1 {
			return 0, ErrInvalid
		}
	}
	return .6*p.Similarity + .25*p.Declaration + .15*p.Structure, nil
}

func navigationRelation(kind string) bool {
	switch kind {
	case "cites", "implements", "template_for":
		return true
	default:
		return false
	}
}

func declarableRelation(kind string) bool {
	return kind == "amends" || kind == "interprets" || kind == "related_to" || navigationRelation(kind)
}

func relationTargetScope(source provenance.Scope, targetKB string) provenance.Scope {
	return provenance.Scope{TenantID: source.TenantID, KnowledgeBaseID: targetKB}
}

func relationTargetKB(c Command) string {
	if c.Payload.TargetKnowledgeBaseID != "" {
		return c.Payload.TargetKnowledgeBaseID
	}
	return c.Scope.KnowledgeBaseID
}

func relationDocumentKey(kb, document string) string {
	return kb + "\x00" + document
}

// AttachmentTemplates returns current, same-library templates for one parent.
func (s *Service) AttachmentTemplates(ctx context.Context, scope provenance.Scope, parent string) ([]retrieval.AttachmentTemplate, error) {
	related, err := s.QueryRelated(ctx, scope, parent)
	if errors.Is(err, ErrConflict) || errors.Is(err, ErrNotFound) || errors.Is(err, lineage.ErrSourceMissing) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var templates []retrieval.AttachmentTemplate
	for _, row := range related {
		if row.KnowledgeBaseID == scope.KnowledgeBaseID && row.EdgeType == "template_for" && row.Direction == "incoming" {
			templates = append(templates, retrieval.AttachmentTemplate{DocumentID: row.DocumentID, Reference: row.Reference})
		}
	}
	return templates, nil
}

func (s *Service) relation(ctx context.Context, c Command) (Result, error) {
	p := c.Payload
	var source, target string
	var targetKB string
	var targetScope provenance.Scope
	var e error
	if c.Action == "relation_declare" {
		if !validID(p.DocumentID) || !validID(p.TargetDocumentID) || p.DocumentID == p.TargetDocumentID || !declarableRelation(p.EdgeType) {
			return Result{}, ErrInvalid
		}
		targetKB = relationTargetKB(c)
		if !validID(targetKB) {
			return Result{}, ErrInvalid
		}
		targetScope = relationTargetScope(c.Scope, targetKB)
		if targetKB != c.Scope.KnowledgeBaseID {
			if navigationRelation(p.EdgeType) {
				return Result{}, ErrInvalid
			}
			if p.EdgeType == "related_to" && !p.SecondConfirmation {
				return Result{}, ErrInvalid
			}
		}
		source, e = s.verify(ctx, c.Scope, p.DocumentID)
		if e != nil {
			return Result{}, e
		}
		target, e = s.verify(ctx, targetScope, p.TargetDocumentID)
		if e != nil {
			return Result{}, e
		}
	}
	return s.transact(ctx, c, func(tx *sql.Tx) (Result, error) {
		out := Result{ID: c.ID, Revision: c.ExpectedRevision + 1}
		var doc, dst, kind, status, sv, tv string
		var rev int64
		var confidence float64
		var edgeID, crossEdgeID sql.NullString
		if c.Action == "relation_declare" {
			if c.ExpectedRevision != 0 {
				return Result{}, ErrConflict
			}
			if e = s.current(ctx, tx, c.Scope, p.DocumentID, source); e != nil {
				return Result{}, e
			}
			if e = s.current(ctx, tx, targetScope, p.TargetDocumentID, target); e != nil {
				return Result{}, e
			}
			if p.EdgeType == "template_for" {
				var blocked bool
				e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM ext_attachment_delete_intents
					WHERE tenant_id=? AND knowledge_base_id=? AND document_id IN (?,?))
					OR EXISTS(SELECT 1 FROM ext_attachment_links WHERE tenant_id=? AND attachment_id=? AND removed_at IS NULL)
					OR EXISTS(SELECT 1 FROM ext_document_relations WHERE tenant_id=? AND knowledge_base_id=? AND target_knowledge_base_id=?
					AND document_id=? AND target_document_id=? AND edge_type='template_for' AND status='active'
					AND (source_version_id=? OR `+store.AttachmentPresentationRevisionSQL("ext_document_relations.source_version_id", "?")+`)
					AND (target_version_id=? OR `+store.AttachmentPresentationRevisionSQL("ext_document_relations.target_version_id", "?")+`))`,
					fmt.Sprint(c.Scope.TenantID), c.Scope.KnowledgeBaseID, p.DocumentID, p.TargetDocumentID,
					fmt.Sprint(c.Scope.TenantID), p.DocumentID, fmt.Sprint(c.Scope.TenantID), c.Scope.KnowledgeBaseID, targetKB,
					p.DocumentID, p.TargetDocumentID, source, source, target, target).Scan(&blocked)
				if e != nil {
					return Result{}, e
				}
				if blocked {
					return Result{}, ErrConflict
				}
			}
			confidence, e = score(p)
			if e != nil {
				return Result{}, e
			}
			out.ID = newID()
			out.Status = "active"
			if p.EdgeType == "related_to" {
				out.Status = "candidate"
			} else {
				confidence = 1
			}
			if _, e = tx.ExecContext(ctx, `INSERT INTO ext_document_relations
				(tenant_id,knowledge_base_id,relation_id,document_id,target_knowledge_base_id,target_document_id,
				 source_version_id,target_version_id,edge_type,status,confidence,revision,edge_id,cross_edge_id)
				VALUES(?,?,?,?,?,?,?,?,?,?,?,1,NULL,NULL)`,
				strconv.FormatUint(c.Scope.TenantID, 10), c.Scope.KnowledgeBaseID, out.ID, p.DocumentID, targetKB,
				p.TargetDocumentID, source, target, p.EdgeType, out.Status, confidence); e != nil {
				return Result{}, e
			}
			doc, dst, kind, sv, tv = p.DocumentID, p.TargetDocumentID, p.EdgeType, source, target
		} else {
			if e = tx.QueryRowContext(ctx, `SELECT document_id,target_document_id,target_knowledge_base_id,edge_type,status,
				source_version_id,target_version_id,revision,confidence,edge_id,cross_edge_id
				FROM ext_document_relations WHERE tenant_id=? AND knowledge_base_id=? AND relation_id=?`,
				args(c.Scope, c.ID)...).Scan(&doc, &dst, &targetKB, &kind, &status, &sv, &tv, &rev, &confidence, &edgeID, &crossEdgeID); e != nil || rev != c.ExpectedRevision {
				return Result{}, ErrConflict
			}
			targetScope = relationTargetScope(c.Scope, targetKB)
			if c.Action == "relation_confirm" {
				if status != "candidate" {
					return Result{}, ErrConflict
				}
				if e = s.current(ctx, tx, c.Scope, doc, sv); e != nil {
					return Result{}, e
				}
				if e = s.current(ctx, tx, targetScope, dst, tv); e != nil {
					return Result{}, e
				}
				out.Status = "active"
			} else {
				if status == "revoked" {
					return Result{}, ErrConflict
				}
				out.Status = "revoked"
			}
			if out.Status == "revoked" {
				if _, e = tx.ExecContext(ctx, `UPDATE ext_document_relations SET status=?,revision=?,edge_id=NULL,cross_edge_id=NULL
					WHERE tenant_id=? AND knowledge_base_id=? AND relation_id=?`,
					append([]any{out.Status, out.Revision}, args(c.Scope, c.ID)...)...); e != nil {
					return Result{}, e
				}
			} else if _, e = tx.ExecContext(ctx, `UPDATE ext_document_relations SET status=?,revision=?
				WHERE tenant_id=? AND knowledge_base_id=? AND relation_id=?`,
				append([]any{out.Status, out.Revision}, args(c.Scope, c.ID)...)...); e != nil {
				return Result{}, e
			}
		}
		if out.Status == "active" || out.Status == "revoked" {
			if e = s.enqueueRelationTagsTx(ctx, tx, c.Scope, doc, out.ID, out.Revision); e != nil {
				return Result{}, e
			}
			if e = s.enqueueRelationTagsTx(ctx, tx, targetScope, dst, out.ID, out.Revision); e != nil {
				return Result{}, e
			}
		}
		// Navigation relationships have a business record and audit receipt,
		// but carry no lifecycle or dependency semantics.
		if navigationRelation(kind) {
			return out, nil
		}
		sameKBEdgeID := relationEdgeID(c.Scope, targetScope, sv, tv, kind)
		if out.Status == "active" {
			e = s.activateRelationEdgeTx(ctx, tx, c.Scope, targetScope, out.ID, sv, tv, kind, sameKBEdgeID)
		} else if out.Status == "revoked" {
			if edgeID.Valid {
				if _, e = tx.ExecContext(ctx, `DELETE FROM ext_lineage_edge WHERE edge_id=? AND NOT EXISTS(
					SELECT 1 FROM ext_document_relations WHERE edge_id=?)`, edgeID.String, edgeID.String); e != nil {
					return Result{}, e
				}
			}
			if crossEdgeID.Valid {
				if _, e = tx.ExecContext(ctx, `DELETE FROM ext_lineage_edge_cross_kb WHERE edge_id=? AND NOT EXISTS(
					SELECT 1 FROM ext_document_relations WHERE cross_edge_id=?)`, crossEdgeID.String, crossEdgeID.String); e != nil {
					return Result{}, e
				}
			}
		}
		return out, e
	})
}

func (s *Service) enqueueRelationTagsTx(ctx context.Context, tx *sql.Tx, scope provenance.Scope, document, relationID string, revision int64) error {
	payload, err := json.Marshal(map[string]string{"relation_id": relationID})
	if err != nil {
		return err
	}
	return outbox.EnqueueTx(ctx, tx, outbox.Item{
		ID:              fmt.Sprintf("relation-tag:%s:%s:%d", relationID, document, revision),
		TenantID:        scope.TenantID,
		KnowledgeBaseID: scope.KnowledgeBaseID,
		ProjectionType:  taggov.RelationProjectionType,
		AggregateType:   "document",
		AggregateID:     document,
		CausationType:   taggov.RelationCausation,
		CausationID:     fmt.Sprintf("%s:%d", relationID, revision),
		Payload:         payload,
		CreatedAt:       s.now().UTC(),
	})
}

func relationEdgeID(source, target provenance.Scope, sourceVersion, targetVersion, kind string) string {
	if source.KnowledgeBaseID == target.KnowledgeBaseID {
		return digest([]string{strconvScope(source), sourceVersion, targetVersion, kind})
	}
	return digest([]string{"cross", strconvScope(source), strconvScope(target), sourceVersion, targetVersion, kind})
}

func (s *Service) activateRelationEdgeTx(ctx context.Context, tx *sql.Tx, source, target provenance.Scope, id, sourceVersion, targetVersion, kind, edgeID string) error {
	if source.KnowledgeBaseID == target.KnowledgeBaseID {
		if _, err := tx.ExecContext(ctx, `INSERT INTO ext_lineage_edge VALUES(?,?,?,?,?,?,'governance_critical',?) ON CONFLICT DO NOTHING`,
			append([]any{edgeID}, args(source, sourceVersion, targetVersion, kind, s.now().UTC().Format(time.RFC3339Nano))...)...); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `UPDATE ext_document_relations SET edge_id=?,cross_edge_id=NULL
			WHERE tenant_id=? AND knowledge_base_id=? AND relation_id=?`,
			append([]any{edgeID}, args(source, id)...)...)
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO ext_lineage_edge_cross_kb
		(edge_id,tenant_id,source_knowledge_base_id,source_node_id,target_knowledge_base_id,target_node_id,edge_type,edge_class,created_at)
		VALUES(?,?,?,?,?,?,?,'governance_critical',?) ON CONFLICT DO NOTHING`,
		edgeID, strconv.FormatUint(source.TenantID, 10), source.KnowledgeBaseID, sourceVersion,
		target.KnowledgeBaseID, targetVersion, kind, s.now().UTC().Format(time.RFC3339Nano)); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `UPDATE ext_document_relations SET cross_edge_id=?,edge_id=NULL
		WHERE tenant_id=? AND knowledge_base_id=? AND relation_id=?`,
		append([]any{edgeID}, args(source, id)...)...)
	return err
}

func strconvScope(s provenance.Scope) string { return fmt.Sprint(s.TenantID, ":", s.KnowledgeBaseID) }

// QueryRelated checks current versions and lifecycle before exposing confirmed edges.
func (s *Service) QueryRelated(ctx context.Context, scope provenance.Scope, doc string) ([]Related, error) {
	if !validID(doc) {
		return nil, ErrInvalid
	}
	version, e := s.verify(ctx, scope, doc)
	if e != nil {
		return nil, e
	}
	type item struct {
		Related
		version string
	}
	scan := func(rows *sql.Rows) (item, error) {
		var v item
		var evidence sql.NullString
		err := rows.Scan(&v.KnowledgeBaseID, &v.DocumentID, &v.EdgeType, &v.Confidence, &v.Direction, &v.version,
			&v.RelationID, &v.Revision, &evidence)
		if err == nil && evidence.Valid {
			err = json.Unmarshal([]byte(evidence.String), &v.Reference)
		}
		return v, err
	}
	items := []item{}
	e = s.db.View(ctx, func(tx *sql.Tx) error {
		if e := s.current(ctx, tx, scope, doc, version); e != nil {
			return e
		}
		rows, e := tx.QueryContext(ctx, relatedSQL, relatedArgs(scope, doc, version)...)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			v, e := scan(rows)
			if e != nil {
				return e
			}
			items = append(items, v)
		}
		return rows.Err()
	})
	if e != nil {
		return nil, e
	}
	if len(items) > 100 {
		return nil, ErrConflict
	}
	verified := map[string]string{}
	for _, v := range items {
		relatedScope := relationTargetScope(scope, v.KnowledgeBaseID)
		current, e := s.verify(ctx, relatedScope, v.DocumentID)
		if errors.Is(e, ErrConflict) || errors.Is(e, lineage.ErrSourceMissing) {
			continue
		}
		if e != nil {
			return nil, e
		}
		if current != v.version && v.EdgeType != "template_for" {
			continue
		}
		verified[relationDocumentKey(v.KnowledgeBaseID, v.DocumentID)] = current
	}
	out := []Related{}
	e = s.db.View(ctx, func(tx *sql.Tx) error {
		if e := s.current(ctx, tx, scope, doc, version); e != nil {
			return e
		}
		rows, e := tx.QueryContext(ctx, relatedSQL, relatedArgs(scope, doc, version)...)
		if e != nil {
			return e
		}
		currentItems := []item{}
		for rows.Next() {
			v, e := scan(rows)
			if e != nil {
				rows.Close()
				return e
			}
			currentItems = append(currentItems, v)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return e
		}
		if len(currentItems) > 100 {
			return ErrConflict
		}
		for _, v := range currentItems {
			current := verified[relationDocumentKey(v.KnowledgeBaseID, v.DocumentID)]
			if current == "" {
				continue
			}
			if current != v.version {
				if v.EdgeType != "template_for" {
					continue
				}
				var presentationOnly bool
				if e = tx.QueryRowContext(ctx, "SELECT "+store.AttachmentPresentationRevisionSQL("?", "?"),
					v.version, current).Scan(&presentationOnly); e != nil {
					return e
				}
				if !presentationOnly {
					continue
				}
			}
			relatedScope := relationTargetScope(scope, v.KnowledgeBaseID)
			if e = s.current(ctx, tx, relatedScope, v.DocumentID, current); e == nil {
				out = append(out, v.Related)
			} else if !errors.Is(e, ErrConflict) {
				return e
			}
		}
		return nil
	})
	if e != nil {
		return nil, e
	}
	return out, nil
}

// A confirmed family suggestion passed the scoring policy recorded at its
// review. Manual relationship candidates retain the original global minimum.
const relatedConfidenceSQL = `(r.edge_type<>'related_to' OR r.confidence>=0.7 OR EXISTS(
	SELECT 1 FROM ext_family_suggestions s WHERE s.tenant_id=r.tenant_id AND s.knowledge_base_id=r.knowledge_base_id
	AND s.applied_id=r.relation_id AND s.edge_type='related_to' AND s.status='confirmed'
	AND s.source_version_id=r.source_version_id AND s.target_version_id=r.target_version_id))`

var relatedSQL = `SELECT target_knowledge_base_id,target_document_id,edge_type,confidence,'outgoing',target_version_id,r.relation_id,r.revision,r.attachment_reference_json FROM ext_document_relations r WHERE tenant_id=? AND knowledge_base_id=? AND document_id=? AND (source_version_id=? OR (edge_type='template_for' AND ` + store.AttachmentPresentationRevisionSQL("r.source_version_id", "?") + `)) AND status='active' AND ` + relatedConfidenceSQL + `
 UNION SELECT knowledge_base_id,document_id,edge_type,confidence,'incoming',source_version_id,r.relation_id,r.revision,r.attachment_reference_json FROM ext_document_relations r WHERE tenant_id=? AND target_knowledge_base_id=? AND target_document_id=? AND (target_version_id=? OR (edge_type='template_for' AND ` + store.AttachmentPresentationRevisionSQL("r.target_version_id", "?") + `)) AND status='active' AND ` + relatedConfidenceSQL + `
 UNION SELECT c.knowledge_base_id,p.parent_document_id,'supplements',1,'outgoing',p.parent_version_node_id,'',0,NULL FROM ext_supplement_current c JOIN ext_supplement_declarations d ON d.tenant_id=c.tenant_id AND d.knowledge_base_id=c.knowledge_base_id AND d.document_id=c.document_id AND d.declaration_id=c.declaration_id AND d.revision=c.revision AND d.status='active' JOIN ext_supplement_parents p ON p.declaration_id=d.declaration_id AND p.tenant_id=d.tenant_id AND p.knowledge_base_id=d.knowledge_base_id WHERE c.tenant_id=? AND c.knowledge_base_id=? AND c.document_id=? AND d.child_version_node_id=?
 UNION SELECT c.knowledge_base_id,c.document_id,'supplements',1,'incoming',d.child_version_node_id,'',0,NULL FROM ext_supplement_current c JOIN ext_supplement_declarations d ON d.tenant_id=c.tenant_id AND d.knowledge_base_id=c.knowledge_base_id AND d.document_id=c.document_id AND d.declaration_id=c.declaration_id AND d.revision=c.revision AND d.status='active' JOIN ext_supplement_parents p ON p.declaration_id=d.declaration_id AND p.tenant_id=d.tenant_id AND p.knowledge_base_id=d.knowledge_base_id WHERE c.tenant_id=? AND c.knowledge_base_id=? AND p.parent_document_id=? AND p.parent_version_node_id=?
 UNION SELECT cp.parent_knowledge_base_id,cp.parent_document_id,'supplements',1,'outgoing',cp.parent_version_node_id,'',0,NULL FROM ext_supplement_current c JOIN ext_supplement_declarations d ON d.tenant_id=c.tenant_id AND d.knowledge_base_id=c.knowledge_base_id AND d.document_id=c.document_id AND d.declaration_id=c.declaration_id AND d.revision=c.revision AND d.status='active' JOIN ext_supplement_cross_parents cp ON cp.declaration_id=d.declaration_id AND cp.tenant_id=d.tenant_id AND cp.knowledge_base_id=d.knowledge_base_id WHERE c.tenant_id=? AND c.knowledge_base_id=? AND c.document_id=? AND d.child_version_node_id=?
 UNION SELECT cp.knowledge_base_id,cp.document_id,'supplements',1,'incoming',d.child_version_node_id,'',0,NULL FROM ext_supplement_current c JOIN ext_supplement_declarations d ON d.tenant_id=c.tenant_id AND d.knowledge_base_id=c.knowledge_base_id AND d.document_id=c.document_id AND d.declaration_id=c.declaration_id AND d.revision=c.revision AND d.status='active' JOIN ext_supplement_cross_parents cp ON cp.declaration_id=d.declaration_id AND cp.tenant_id=d.tenant_id AND cp.knowledge_base_id=d.knowledge_base_id WHERE cp.tenant_id=? AND cp.parent_knowledge_base_id=? AND cp.parent_document_id=? AND cp.parent_version_node_id=? LIMIT 101`

func relatedArgs(scope provenance.Scope, doc, version string) []any {
	out := []any{}
	for i := 0; i < 6; i++ {
		out = append(out, args(scope, doc, version)...)
		if i < 2 {
			out = append(out, version)
		}
	}
	return out
}
