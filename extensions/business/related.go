package business

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/ybochuan/LanRAG-Next/extensions/provenance"
	"math"
	"time"
)

// Related is a same-KB, currently active relationship without document content.
type Related struct {
	DocumentID string  `json:"document_id"`
	EdgeType   string  `json:"edge_type"`
	Confidence float64 `json:"confidence"`
	Direction  string  `json:"direction"`
}

func score(p Payload) (float64, error) {
	for _, v := range []float64{p.Similarity, p.Declaration, p.Structure} {
		if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 || v > 1 {
			return 0, ErrInvalid
		}
	}
	return .6*p.Similarity + .25*p.Declaration + .15*p.Structure, nil
}
func (s *Service) relation(ctx context.Context, c Command) (Result, error) {
	p := c.Payload
	var source, target string
	var e error
	if c.Action == "relation_declare" {
		if !validID(p.DocumentID) || !validID(p.TargetDocumentID) || p.DocumentID == p.TargetDocumentID || p.EdgeType != "amends" && p.EdgeType != "interprets" && p.EdgeType != "related_to" {
			return Result{}, ErrInvalid
		}
		source, e = s.verify(ctx, c.Scope, p.DocumentID)
		if e != nil {
			return Result{}, e
		}
		target, e = s.verify(ctx, c.Scope, p.TargetDocumentID)
		if e != nil {
			return Result{}, e
		}
	}
	return s.transact(ctx, c, func(tx *sql.Tx) (Result, error) {
		out := Result{ID: c.ID, Revision: c.ExpectedRevision + 1}
		var doc, dst, kind, status, sv, tv string
		var rev int64
		var confidence float64
		if c.Action == "relation_declare" {
			if c.ExpectedRevision != 0 {
				return Result{}, ErrConflict
			}
			if e = s.current(ctx, tx, c.Scope, p.DocumentID, source); e != nil {
				return Result{}, e
			}
			if e = s.current(ctx, tx, c.Scope, p.TargetDocumentID, target); e != nil {
				return Result{}, e
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
			if _, e = tx.ExecContext(ctx, `INSERT INTO ext_document_relations VALUES(?,?,?,?,?,?,?,?,?,?,1,NULL)`, args(c.Scope, out.ID, p.DocumentID, p.TargetDocumentID, source, target, p.EdgeType, out.Status, confidence)...); e != nil {
				return Result{}, e
			}
			doc, dst, kind, sv, tv = p.DocumentID, p.TargetDocumentID, p.EdgeType, source, target
		} else {
			if e = tx.QueryRowContext(ctx, `SELECT document_id,target_document_id,edge_type,status,source_version_id,target_version_id,revision,confidence FROM ext_document_relations WHERE tenant_id=? AND knowledge_base_id=? AND relation_id=?`, args(c.Scope, c.ID)...).Scan(&doc, &dst, &kind, &status, &sv, &tv, &rev, &confidence); e != nil || rev != c.ExpectedRevision {
				return Result{}, ErrConflict
			}
			if c.Action == "relation_confirm" {
				if status != "candidate" {
					return Result{}, ErrConflict
				}
				if e = s.current(ctx, tx, c.Scope, doc, sv); e != nil {
					return Result{}, e
				}
				if e = s.current(ctx, tx, c.Scope, dst, tv); e != nil {
					return Result{}, e
				}
				out.Status = "active"
			} else {
				if status == "revoked" {
					return Result{}, ErrConflict
				}
				out.Status = "revoked"
			}
			if _, e = tx.ExecContext(ctx, `UPDATE ext_document_relations SET status=?,revision=? WHERE tenant_id=? AND knowledge_base_id=? AND relation_id=?`, append([]any{out.Status, out.Revision}, args(c.Scope, c.ID)...)...); e != nil {
				return Result{}, e
			}
		}
		edgeID := digest([]string{strconvScope(c.Scope), sv, tv, kind})
		if out.Status == "active" {
			if _, e = tx.ExecContext(ctx, `INSERT INTO ext_lineage_edge VALUES(?,?,?,?,?,?,'governance_critical',?) ON CONFLICT DO NOTHING`, append([]any{edgeID}, args(c.Scope, sv, tv, kind, s.now().UTC().Format(time.RFC3339Nano))...)...); e != nil {
				return Result{}, e
			}
			_, e = tx.ExecContext(ctx, `UPDATE ext_document_relations SET edge_id=? WHERE tenant_id=? AND knowledge_base_id=? AND relation_id=?`, append([]any{edgeID}, args(c.Scope, out.ID)...)...)
		} else if out.Status == "revoked" {
			if _, e = tx.ExecContext(ctx, `UPDATE ext_document_relations SET edge_id=NULL WHERE tenant_id=? AND knowledge_base_id=? AND relation_id=?`, args(c.Scope, out.ID)...); e != nil {
				return Result{}, e
			}
			_, e = tx.ExecContext(ctx, `DELETE FROM ext_lineage_edge WHERE edge_id=? AND NOT EXISTS(SELECT 1 FROM ext_document_relations WHERE edge_id=?)`, edgeID, edgeID)
		}
		return out, e
	})
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
			var v item
			if e = rows.Scan(&v.DocumentID, &v.EdgeType, &v.Confidence, &v.Direction, &v.version); e != nil {
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
		current, e := s.verify(ctx, scope, v.DocumentID)
		if errors.Is(e, ErrConflict) {
			continue
		}
		if e != nil {
			return nil, e
		}
		if current != v.version {
			continue
		}
		verified[v.DocumentID] = current
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
			var v item
			if e = rows.Scan(&v.DocumentID, &v.EdgeType, &v.Confidence, &v.Direction, &v.version); e != nil {
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
			if verified[v.DocumentID] != v.version {
				continue
			}
			if e = s.current(ctx, tx, scope, v.DocumentID, v.version); e == nil {
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

const relatedSQL = `SELECT target_document_id,edge_type,confidence,'outgoing',target_version_id FROM ext_document_relations WHERE tenant_id=? AND knowledge_base_id=? AND document_id=? AND source_version_id=? AND status='active' AND (edge_type<>'related_to' OR confidence>=0.7)
 UNION SELECT document_id,edge_type,confidence,'incoming',source_version_id FROM ext_document_relations WHERE tenant_id=? AND knowledge_base_id=? AND target_document_id=? AND target_version_id=? AND status='active' AND (edge_type<>'related_to' OR confidence>=0.7)
 UNION SELECT p.parent_document_id,'supplements',1,'outgoing',p.parent_version_node_id FROM ext_supplement_current c JOIN ext_supplement_declarations d ON d.declaration_id=c.declaration_id AND d.status='active' JOIN ext_supplement_parents p ON p.declaration_id=d.declaration_id WHERE c.tenant_id=? AND c.knowledge_base_id=? AND c.document_id=? AND d.child_version_node_id=?
 UNION SELECT c.document_id,'supplements',1,'incoming',d.child_version_node_id FROM ext_supplement_current c JOIN ext_supplement_declarations d ON d.declaration_id=c.declaration_id AND d.status='active' JOIN ext_supplement_parents p ON p.declaration_id=d.declaration_id WHERE c.tenant_id=? AND c.knowledge_base_id=? AND p.parent_document_id=? AND p.parent_version_node_id=? LIMIT 101`

func relatedArgs(scope provenance.Scope, doc, version string) []any {
	out := []any{}
	for i := 0; i < 4; i++ {
		out = append(out, args(scope, doc, version)...)
	}
	return out
}
