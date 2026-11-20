package lineage

import (
	"context"
	"database/sql"
	"encoding/json"
	"strconv"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/provenance"
	"github.com/ybochuan/LanRAG-Next/extensions/store"
)

// GraphNode describes registered metadata. State is the document's current lifecycle.
type GraphNode struct {
	ID                 string    `json:"id"`
	Type               string    `json:"type"`
	KnowledgeBaseID    string    `json:"knowledge_base_id,omitempty"`
	DocumentID         string    `json:"document_id"`
	FamilyID           string    `json:"family_id,omitempty"`
	ObservationChainID string    `json:"observation_chain_id,omitempty"`
	BusinessFamilyID   string    `json:"business_family_id,omitempty"`
	Sequence           int64     `json:"sequence"`
	Current            bool      `json:"current"`
	State              string    `json:"state"`
	Availability       string    `json:"availability,omitempty"`
	CreatedAt          time.Time `json:"created_at"`
	Title              string    `json:"title,omitempty"`
}

// GraphEdge preserves the stored direction, including derived node to source.
type GraphEdge struct {
	ID     string `json:"id"`
	From   string `json:"from"`
	To     string `json:"to"`
	Type   string `json:"type"`
	Status string `json:"status,omitempty"`
}

// GraphSnapshot is a bounded, same-KB snapshot. Truncation is always explicit.
type GraphSnapshot struct {
	Mode              string      `json:"mode"`
	RootID            string      `json:"root_id"`
	Nodes             []GraphNode `json:"nodes"`
	Edges             []GraphEdge `json:"edges"`
	Truncated         bool        `json:"truncated"`
	TitlesUnavailable bool        `json:"titles_unavailable"`
}

// Graph reads actual edges and metadata in one transaction. A root narrows
// provenance or version ancestry using the existing cycle-guarded traversal.
func (r *Registry) Graph(ctx context.Context, s provenance.Scope, mode, root string) (GraphSnapshot, error) {
	out := GraphSnapshot{Mode: mode, RootID: root, Nodes: []GraphNode{}, Edges: []GraphEdge{}}
	if r == nil || r.db == nil || s.TenantID == 0 || !validID(s.KnowledgeBaseID) || root != "" && !validID(root) {
		return out, ErrEvidence
	}
	includeCross := mode == "relations-cross"
	if includeCross {
		mode = "relations"
	}
	edges, condition, edgeStatus := "ext_lineage_edge", "", "''"
	switch mode {
	case "renditions":
		return r.renditionGraph(ctx, s, out)
	case "provenance":
		condition = " AND e.edge_type='derived_from'"
	case "version-ancestry":
		edges = versionAncestryEdges
	case "relations":
		edgeStatus = "e.status"
		edges = `(
			SELECT e.edge_id,e.tenant_id,e.knowledge_base_id,e.from_node_id,e.to_node_id,e.edge_type,e.created_at,'' AS status
			FROM ext_lineage_edge e WHERE e.edge_type IN ('amends','interprets','related_to')
			UNION ALL
			SELECT e.edge_id,e.tenant_id,e.knowledge_base_id,e.from_node_id,e.to_node_id,e.edge_type,e.created_at,'' AS status
			FROM ext_lineage_edge e WHERE e.edge_type='supplements' AND EXISTS(
				SELECT 1 FROM ext_supplement_edge_facts f
				JOIN ext_supplement_current c USING(tenant_id,knowledge_base_id,document_id,declaration_id)
				JOIN ext_supplement_declarations d USING(tenant_id,knowledge_base_id,document_id,declaration_id,revision)
				WHERE f.tenant_id=e.tenant_id AND f.knowledge_base_id=e.knowledge_base_id AND f.edge_id=e.edge_id
				AND d.status='active')
			UNION ALL
			SELECT edge_id,tenant_id,knowledge_base_id,from_node_id,to_node_id,edge_type,created_at,'' FROM ext_repeal_edges
			UNION ALL
			SELECT edge_id,tenant_id,knowledge_base_id,from_node_id,to_node_id,edge_type,created_at,status FROM ext_clause_edges
		)`
		if root != "" {
			edges = "(SELECT * FROM " + edges + " UNION ALL SELECT v.*,'' AS status FROM " + versionAncestryEdges + " v)"
		}
	default:
		return out, ErrEvidence
	}
	err := r.db.View(ctx, func(tx *sql.Tx) error {
		where := ""
		params := scopeArgs(s)
		if root != "" && mode == "relations" {
			nodes, err := documentGraphNodes(ctx, tx, s, root, edges)
			if err != nil {
				return err
			}
			body, _ := json.Marshal(nodes)
			where = " AND n.node_id IN (SELECT value FROM json_each(?))"
			params = append(params, string(body))
		} else if root != "" {
			ancestors, err := traverse(ctx, tx, s, root, mode)
			if err != nil {
				return err
			}
			ids := []string{root}
			for _, ancestor := range ancestors {
				ids = append(ids, ancestor.NodeID)
			}
			body, _ := json.Marshal(ids)
			where = " AND n.node_id IN (SELECT value FROM json_each(?))"
			params = append(params, string(body))
		} else if mode != "provenance" {
			where = " AND n.node_type='document_version'"
		}
		rows, err := tx.QueryContext(ctx, `SELECT n.node_id,n.node_type,n.knowledge_base_id,
CASE WHEN n.node_type='document_version' THEN coalesce(v.document_id,n.native_id) ELSE '' END,
coalesce(v.family_id,''),coalesce(m.business_family_id,''),coalesce(v.observation_sequence,0),
coalesce(f.current_version_node_id=n.node_id,0),coalesce(c.state,''),coalesce(av.status,''),n.created_at
FROM ext_lineage_node n
LEFT JOIN ext_document_version v ON v.tenant_id=n.tenant_id AND v.knowledge_base_id=n.knowledge_base_id AND v.version_node_id=n.node_id
LEFT JOIN ext_document_version_family f ON f.tenant_id=v.tenant_id AND f.knowledge_base_id=v.knowledge_base_id AND f.family_id=v.family_id
LEFT JOIN ext_lifecycle_current c ON c.tenant_id=v.tenant_id AND c.knowledge_base_id=v.knowledge_base_id AND c.document_id=v.document_id
LEFT JOIN ext_document_availability av ON av.tenant_id=v.tenant_id AND av.knowledge_base_id=v.knowledge_base_id AND av.document_id=v.document_id
LEFT JOIN ext_business_document_family_members m ON m.tenant_id=v.tenant_id AND m.knowledge_base_id=v.knowledge_base_id AND m.document_id=v.document_id
WHERE n.tenant_id=? AND n.knowledge_base_id=?`+where+" ORDER BY n.created_at DESC,n.node_id LIMIT 257", params...)
		if err != nil {
			return err
		}
		for rows.Next() {
			var node GraphNode
			var created string
			if err := rows.Scan(&node.ID, &node.Type, &node.KnowledgeBaseID, &node.DocumentID, &node.FamilyID, &node.BusinessFamilyID, &node.Sequence, &node.Current, &node.State, &node.Availability, &created); err != nil {
				rows.Close()
				return err
			}
			createdAt, err := time.Parse(time.RFC3339Nano, created)
			if err != nil {
				rows.Close()
				return ErrUnavailable
			}
			node.CreatedAt = createdAt.UTC()
			node.ObservationChainID = node.FamilyID
			if mode == "relations" && root != "" {
				node.FamilyID, node.ObservationChainID = "", ""
			}
			if len(out.Nodes) == 256 {
				out.Truncated = true
				break
			}
			out.Nodes = append(out.Nodes, node)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		ids := make([]string, 0, len(out.Nodes))
		for _, node := range out.Nodes {
			ids = append(ids, node.ID)
		}
		body, _ := json.Marshal(ids)
		params = scopeArgs(s, string(body), string(body))
		rows, err = tx.QueryContext(ctx, `SELECT e.edge_id,e.from_node_id,e.to_node_id,e.edge_type,`+edgeStatus+` FROM `+edges+` e
WHERE e.tenant_id=? AND e.knowledge_base_id=?`+condition+`
AND e.from_node_id IN (SELECT value FROM json_each(?)) AND e.to_node_id IN (SELECT value FROM json_each(?))
ORDER BY e.created_at,e.edge_id LIMIT 513`, params...)
		if err != nil {
			return err
		}
		for rows.Next() {
			var edge GraphEdge
			if err := rows.Scan(&edge.ID, &edge.From, &edge.To, &edge.Type, &edge.Status); err != nil {
				rows.Close()
				return err
			}
			if len(out.Edges) == 512 {
				out.Truncated = true
				break
			}
			out.Edges = append(out.Edges, edge)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		if mode == "relations" {
			if len(ids) > 0 {
				if err := appendNavigationEdges(ctx, tx, s, string(body), &out); err != nil {
					return err
				}
			}
			if err := appendAttachmentEdges(ctx, tx, s, root, &out); err != nil {
				return err
			}
			if includeCross && len(ids) > 0 {
				return appendCrossKBEges(ctx, tx, s, r.allowed, ids, &out)
			}
		}
		return nil
	})
	return out, err
}

// documentGraphNodes seeds a document's neighbourhood before the graph limit
// is applied, including both ends of its publication and current attachment links.
func documentGraphNodes(ctx context.Context, tx *sql.Tx, s provenance.Scope, doc, edges string) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `WITH
scope(tenant_id,knowledge_base_id,document_id) AS (VALUES(?,?,?)),
versions AS (SELECT v.version_node_id FROM ext_document_version v JOIN scope USING(tenant_id,knowledge_base_id,document_id)),
neighbours AS (
 SELECT from_node_id AS id FROM `+edges+` e JOIN scope USING(tenant_id,knowledge_base_id)
 WHERE to_node_id IN (SELECT version_node_id FROM versions)
 UNION
 SELECT to_node_id FROM `+edges+` e JOIN scope USING(tenant_id,knowledge_base_id)
 WHERE from_node_id IN (SELECT version_node_id FROM versions)
 UNION
 SELECT r.source_version_id FROM ext_document_relations r JOIN scope s USING(tenant_id,knowledge_base_id)
 WHERE r.target_document_id=s.document_id AND r.status='active'
 UNION
 SELECT r.target_version_id FROM ext_document_relations r JOIN scope s USING(tenant_id,knowledge_base_id)
 WHERE r.document_id=s.document_id AND r.status='active'
 UNION
 SELECT f.current_version_node_id FROM ext_attachment_links a JOIN scope s USING(tenant_id,knowledge_base_id)
 JOIN ext_document_version_family f ON f.tenant_id=s.tenant_id AND f.knowledge_base_id=s.knowledge_base_id
 AND f.document_id=CASE WHEN a.attachment_id=s.document_id THEN a.parent_document_id ELSE a.attachment_id END
 WHERE (a.attachment_id=s.document_id OR a.parent_document_id=s.document_id)
 AND a.removed_at IS NULL AND a.retired_at IS NULL
)
SELECT version_node_id FROM versions UNION SELECT id FROM neighbours`, scopeArgs(s, doc)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func appendNavigationEdges(ctx context.Context, tx *sql.Tx, s provenance.Scope, ids string, out *GraphSnapshot) error {
	rows, err := tx.QueryContext(ctx, `SELECT 'relation:'||r.relation_id,r.source_version_id,r.target_version_id,r.edge_type,
		r.document_id,r.target_document_id
	FROM ext_document_relations r
	WHERE r.tenant_id=? AND r.knowledge_base_id=? AND r.target_knowledge_base_id=r.knowledge_base_id AND r.status='active'
	AND r.edge_type IN ('cites','implements','template_for','explains_more','reading_note')
	AND r.source_version_id IN (SELECT value FROM json_each(?))
	AND r.target_version_id IN (SELECT value FROM json_each(?))
	ORDER BY r.relation_id LIMIT 513`, scopeArgs(s, ids, ids)...)
	if err != nil {
		return err
	}
	defer rows.Close()
	now := time.Now().UTC()
	for rows.Next() {
		if len(out.Edges) == 512 {
			out.Truncated = true
			break
		}
		var edge GraphEdge
		source := RelationPin{KnowledgeBaseID: s.KnowledgeBaseID}
		target := RelationPin{KnowledgeBaseID: s.KnowledgeBaseID}
		if err := rows.Scan(&edge.ID, &edge.From, &edge.To, &edge.Type, &source.DocumentID, &target.DocumentID); err != nil {
			return err
		}
		source.VersionID, target.VersionID = edge.From, edge.To
		reason, err := RelationReviewReasonTx(ctx, tx, s.TenantID, edge.Type, source, target, now)
		if err != nil {
			return err
		}
		edge.Status = "registered"
		if reason != "" && reason != "source_unverified" && reason != "target_unverified" {
			edge.Status = "unavailable"
		}
		out.Edges = append(out.Edges, edge)
	}
	return rows.Err()
}

// RelationPin identifies a document and the version used when the relationship was registered.
type RelationPin struct {
	KnowledgeBaseID string
	DocumentID      string
	VersionID       string
}

// RelationReviewReasonTx reports the first known blocker in the current local
// evidence, or an unverified endpoint when required evidence is missing.
// An empty reason provides no live permission or guarantee of current validity.
func RelationReviewReasonTx(ctx context.Context, tx *sql.Tx, tenant uint64, kind string, source, target RelationPin, now time.Time) (string, error) {
	unknown := ""
	for _, endpoint := range []struct {
		prefix string
		pin    RelationPin
	}{{"source_", source}, {"target_", target}} {
		reason, err := relationEndpointReasonTx(ctx, tx, tenant, kind, endpoint.pin, now)
		if err != nil {
			return "", err
		}
		if reason == "unverified" {
			if unknown == "" {
				unknown = endpoint.prefix + reason
			}
		} else if reason != "" {
			return endpoint.prefix + reason, nil
		}
	}
	return unknown, nil
}

func relationEndpointReasonTx(ctx context.Context, tx *sql.Tx, tenant uint64, kind string, pin RelationPin, now time.Time) (string, error) {
	scope := provenance.Scope{TenantID: tenant, KnowledgeBaseID: pin.KnowledgeBaseID}
	var head, state, availability, from, until sql.NullString
	var held bool
	err := tx.QueryRowContext(ctx, `SELECT f.current_version_node_id,
		CASE WHEN `+store.LifecycleCurrentVerifiedSQL+` THEN c.state END,
		av.status,a.effective_from,a.effective_until,
		(`+store.RepealDueSQL+` OR `+store.PublicationTargetPendingSQL+` OR `+store.FamilyReplacementDueSQL+`)
	FROM (SELECT ? AS tenant_id,? AS knowledge_base_id,? AS document_id) pin
	LEFT JOIN ext_document_version_family f USING(tenant_id,knowledge_base_id,document_id)
	LEFT JOIN ext_lifecycle_current c USING(tenant_id,knowledge_base_id,document_id)
	LEFT JOIN ext_document_availability av USING(tenant_id,knowledge_base_id,document_id)
	`+store.LifecycleEventJoinSQL+store.LifecycleAnnotationJoinSQL,
		append([]any{now.UnixMilli()}, scopeArgs(scope, pin.DocumentID)...)...).Scan(&head, &state, &availability, &from, &until, &held)
	if err != nil {
		return "", err
	}
	if availability.String == "missing" || availability.String == "deleted" || held {
		return "unavailable", nil
	}
	if head.Valid && head.String != pin.VersionID {
		var presentationOnly bool
		if kind == "template_for" {
			if err := tx.QueryRowContext(ctx, "SELECT "+store.AttachmentPresentationRevisionSQL("?", "?"),
				pin.VersionID, head.String).Scan(&presentationOnly); err != nil {
				return "", err
			}
		}
		if !presentationOnly {
			return "version_changed", nil
		}
	}
	if state.Valid && state.String != "active" {
		return "inactive", nil
	}
	if effective, err := store.EffectiveAt(now, sql.NullString{}, until); err != nil {
		return "unverified", nil
	} else if !effective {
		return "expired", nil
	}
	if effective, err := store.EffectiveAt(now, from, sql.NullString{}); err != nil {
		return "unverified", nil
	} else if !effective {
		return "not_yet_effective", nil
	}
	if !head.Valid || !state.Valid {
		return "unverified", nil
	}
	return "", nil
}

// appendAttachmentEdges projects stored ownership only onto current observed
// versions whose file fingerprints match the binding.
func appendCrossKBEges(ctx context.Context, tx *sql.Tx, s provenance.Scope, allowed map[string]bool, ids []string, out *GraphSnapshot) error {
	body, _ := json.Marshal(ids)
	tenant := strconv.FormatUint(s.TenantID, 10)
	rows, err := tx.QueryContext(ctx, `SELECT edge_id,source_knowledge_base_id,source_node_id,target_knowledge_base_id,target_node_id,edge_type
FROM ext_lineage_edge_cross_kb
WHERE tenant_id=? AND (
 (source_knowledge_base_id=? AND source_node_id IN (SELECT value FROM json_each(?)))
 OR (target_knowledge_base_id=? AND target_node_id IN (SELECT value FROM json_each(?)))
)
UNION ALL
SELECT e.edge_id,e.source_knowledge_base_id,e.source_node_id,e.target_knowledge_base_id,e.target_node_id,'supplements'
FROM ext_supplement_cross_edges e
JOIN ext_supplement_cross_edge_facts f ON f.tenant_id=e.tenant_id AND f.cross_edge_id=e.edge_id
JOIN ext_supplement_current c ON c.tenant_id=f.tenant_id AND c.knowledge_base_id=f.knowledge_base_id AND c.document_id=f.document_id AND c.declaration_id=f.declaration_id
JOIN ext_supplement_declarations d ON d.tenant_id=c.tenant_id AND d.knowledge_base_id=c.knowledge_base_id AND d.document_id=c.document_id AND d.declaration_id=c.declaration_id AND d.revision=c.revision
WHERE e.tenant_id=? AND d.status='active' AND (
 (e.source_knowledge_base_id=? AND e.source_node_id IN (SELECT value FROM json_each(?)))
 OR (e.target_knowledge_base_id=? AND e.target_node_id IN (SELECT value FROM json_each(?)))
)
LIMIT 513`,
		tenant, s.KnowledgeBaseID, string(body), s.KnowledgeBaseID, string(body),
		tenant, s.KnowledgeBaseID, string(body), s.KnowledgeBaseID, string(body))
	if err != nil {
		return err
	}
	type crossEdge struct {
		id, sourceKB, sourceNode, targetKB, targetNode, edgeType string
	}
	cross := []crossEdge{}
	byKB := map[string][]string{}
	seenNode := map[string]bool{}
	known := map[string]bool{}
	for _, node := range out.Nodes {
		known[node.ID] = true
	}
	for rows.Next() {
		var edge crossEdge
		if err := rows.Scan(&edge.id, &edge.sourceKB, &edge.sourceNode, &edge.targetKB, &edge.targetNode, &edge.edgeType); err != nil {
			rows.Close()
			return err
		}
		if allowed != nil && (!allowed[edge.sourceKB] || !allowed[edge.targetKB]) {
			continue
		}
		cross = append(cross, edge)
		for _, ref := range [][2]string{{edge.sourceKB, edge.sourceNode}, {edge.targetKB, edge.targetNode}} {
			key := ref[0] + "\x00" + ref[1]
			if !seenNode[key] {
				seenNode[key] = true
				byKB[ref[0]] = append(byKB[ref[0]], ref[1])
			}
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for kb, nodeIDs := range byKB {
		nodes, err := loadCrossNodes(ctx, tx, s.TenantID, kb, nodeIDs)
		if err != nil {
			return err
		}
		for _, node := range nodes {
			if known[node.ID] {
				continue
			}
			if len(out.Nodes) == 256 {
				out.Truncated = true
				break
			}
			known[node.ID] = true
			out.Nodes = append(out.Nodes, node)
		}
	}
	for _, edge := range cross {
		if !known[edge.sourceNode] || !known[edge.targetNode] {
			continue
		}
		if len(out.Edges) == 512 {
			out.Truncated = true
			break
		}
		out.Edges = append(out.Edges, GraphEdge{ID: edge.id, From: edge.sourceNode, To: edge.targetNode, Type: edge.edgeType})
	}
	return nil
}

func loadCrossNodes(ctx context.Context, tx *sql.Tx, tenant uint64, kb string, ids []string) ([]GraphNode, error) {
	body, _ := json.Marshal(ids)
	rows, err := tx.QueryContext(ctx, `SELECT n.node_id,n.node_type,n.knowledge_base_id,
CASE WHEN n.node_type='document_version' THEN coalesce(v.document_id,n.native_id) ELSE '' END,
coalesce(v.family_id,''),coalesce(m.business_family_id,''),coalesce(v.observation_sequence,0),
coalesce(f.current_version_node_id=n.node_id,0),coalesce(c.state,''),coalesce(av.status,''),n.created_at
FROM ext_lineage_node n
LEFT JOIN ext_document_version v ON v.tenant_id=n.tenant_id AND v.knowledge_base_id=n.knowledge_base_id AND v.version_node_id=n.node_id
LEFT JOIN ext_document_version_family f ON f.tenant_id=v.tenant_id AND f.knowledge_base_id=v.knowledge_base_id AND f.family_id=v.family_id
LEFT JOIN ext_lifecycle_current c ON c.tenant_id=v.tenant_id AND c.knowledge_base_id=v.knowledge_base_id AND c.document_id=v.document_id
LEFT JOIN ext_document_availability av ON av.tenant_id=v.tenant_id AND av.knowledge_base_id=v.knowledge_base_id AND av.document_id=v.document_id
LEFT JOIN ext_business_document_family_members m ON m.tenant_id=v.tenant_id AND m.knowledge_base_id=v.knowledge_base_id AND m.document_id=v.document_id
WHERE n.tenant_id=? AND n.knowledge_base_id=? AND n.node_id IN (SELECT value FROM json_each(?))
ORDER BY n.created_at DESC,n.node_id`, strconv.FormatUint(tenant, 10), kb, string(body))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	nodes := []GraphNode{}
	for rows.Next() {
		var node GraphNode
		var created string
		if err := rows.Scan(&node.ID, &node.Type, &node.KnowledgeBaseID, &node.DocumentID, &node.FamilyID, &node.BusinessFamilyID, &node.Sequence, &node.Current, &node.State, &node.Availability, &created); err != nil {
			return nil, err
		}
		createdAt, err := time.Parse(time.RFC3339Nano, created)
		if err != nil {
			return nil, ErrUnavailable
		}
		node.CreatedAt = createdAt.UTC()
		node.ObservationChainID = node.FamilyID
		node.FamilyID, node.ObservationChainID = "", ""
		nodes = append(nodes, node)
	}
	return nodes, rows.Err()
}

func appendAttachmentEdges(ctx context.Context, tx *sql.Tx, s provenance.Scope, root string, out *GraphSnapshot) error {
	rows, err := tx.QueryContext(ctx, `SELECT a.attachment_id,a.parent_document_id,a.created_at,
	a.attachment_file_hash,a.parent_file_hash,
	cf.current_version_node_id,pf.current_version_node_id,
	ce.snapshot_json,pe.snapshot_json,
	COALESCE(cs.state,''),COALESCE(ps.state,''),
	COALESCE(ca.status,''),COALESCE(pa.status,'')
	FROM ext_attachment_links a
	LEFT JOIN ext_document_version_family cf ON cf.tenant_id=a.tenant_id AND cf.knowledge_base_id=a.knowledge_base_id AND cf.document_id=a.attachment_id
	LEFT JOIN ext_document_version_family pf ON pf.tenant_id=a.tenant_id AND pf.knowledge_base_id=a.knowledge_base_id AND pf.document_id=a.parent_document_id
	LEFT JOIN ext_source_snapshot_evidence ce ON ce.tenant_id=cf.tenant_id AND ce.knowledge_base_id=cf.knowledge_base_id AND ce.version_node_id=cf.current_version_node_id
	LEFT JOIN ext_source_snapshot_evidence pe ON pe.tenant_id=pf.tenant_id AND pe.knowledge_base_id=pf.knowledge_base_id AND pe.version_node_id=pf.current_version_node_id
	LEFT JOIN ext_lifecycle_current cs ON cs.tenant_id=a.tenant_id AND cs.knowledge_base_id=a.knowledge_base_id AND cs.document_id=a.attachment_id
	LEFT JOIN ext_lifecycle_current ps ON ps.tenant_id=a.tenant_id AND ps.knowledge_base_id=a.knowledge_base_id AND ps.document_id=a.parent_document_id
	LEFT JOIN ext_document_availability ca ON ca.tenant_id=a.tenant_id AND ca.knowledge_base_id=a.knowledge_base_id AND ca.document_id=a.attachment_id
	LEFT JOIN ext_document_availability pa ON pa.tenant_id=a.tenant_id AND pa.knowledge_base_id=a.knowledge_base_id AND pa.document_id=a.parent_document_id
	WHERE a.tenant_id=? AND a.knowledge_base_id=? AND a.removed_at IS NULL AND a.retired_at IS NULL
	AND NOT EXISTS (SELECT 1 FROM ext_attachment_delete_intents d WHERE d.tenant_id=a.tenant_id
	AND d.knowledge_base_id=a.knowledge_base_id AND d.document_id=a.parent_document_id)
	AND (?='' OR a.attachment_id=? OR a.parent_document_id=?)
	ORDER BY a.attachment_id`, scopeArgs(s, root, root, root)...)
	if err != nil {
		return err
	}
	type link struct {
		childID, parentID, created, childHash, parentHash          string
		childState, parentState, childAvail, parentAvail           string
		childVersion, parentVersion, childEvidence, parentEvidence sql.NullString
	}
	links := []link{}
	missing := map[string]bool{}
	known := map[string]bool{}
	for _, node := range out.Nodes {
		known[node.ID] = true
	}
	for rows.Next() {
		var v link
		if err := rows.Scan(&v.childID, &v.parentID, &v.created, &v.childHash, &v.parentHash,
			&v.childVersion, &v.parentVersion, &v.childEvidence, &v.parentEvidence,
			&v.childState, &v.parentState, &v.childAvail, &v.parentAvail); err != nil {
			rows.Close()
			return err
		}
		links = append(links, v)
		if v.childVersion.Valid && v.childVersion.String != "" && !known[v.childVersion.String] {
			missing[v.childVersion.String] = true
		}
		if v.parentVersion.Valid && v.parentVersion.String != "" && !known[v.parentVersion.String] {
			missing[v.parentVersion.String] = true
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	if len(missing) > 0 {
		ids := make([]string, 0, len(missing))
		for id := range missing {
			ids = append(ids, id)
		}
		nodes, err := loadCrossNodes(ctx, tx, s.TenantID, s.KnowledgeBaseID, ids)
		if err != nil {
			return err
		}
		for _, node := range nodes {
			addGraphNode(out, known, node)
		}
	}
	byDoc := map[string]string{}
	for _, node := range out.Nodes {
		if node.DocumentID != "" && node.Current && node.Type == "document_version" {
			byDoc[node.DocumentID] = node.ID
		}
	}
	for _, v := range links {
		childNode := byDoc[v.childID]
		parentNode := byDoc[v.parentID]
		childCurrent := v.childVersion.Valid && v.childVersion.String != ""
		parentCurrent := v.parentVersion.Valid && v.parentVersion.String != ""
		if childCurrent && !attachmentEvidenceMatches(v.childEvidence, v.childHash) {
			continue
		}
		if parentCurrent && !attachmentEvidenceMatches(v.parentEvidence, v.parentHash) {
			continue
		}
		if !childCurrent || childNode == "" {
			childNode = "document:" + v.childID
			if !known[childNode] {
				if len(out.Nodes) == 256 {
					out.Truncated = true
					break
				}
				created, err := time.Parse(time.RFC3339Nano, v.created)
				if err != nil {
					return ErrUnavailable
				}
				addGraphNode(out, known, GraphNode{ID: childNode, Type: "document", KnowledgeBaseID: s.KnowledgeBaseID,
					DocumentID: v.childID, Current: true, State: v.childState, Availability: v.childAvail, CreatedAt: created.UTC()})
			}
		}
		if !parentCurrent || parentNode == "" {
			parentNode = "document:" + v.parentID
			if !known[parentNode] {
				if len(out.Nodes) == 256 {
					out.Truncated = true
					break
				}
				created, err := time.Parse(time.RFC3339Nano, v.created)
				if err != nil {
					return ErrUnavailable
				}
				addGraphNode(out, known, GraphNode{ID: parentNode, Type: "document", KnowledgeBaseID: s.KnowledgeBaseID,
					DocumentID: v.parentID, Current: true, State: v.parentState, Availability: v.parentAvail, CreatedAt: created.UTC()})
			}
		}
		if len(out.Edges) == 512 {
			out.Truncated = true
			break
		}
		out.Edges = append(out.Edges, GraphEdge{ID: "attachment:" + v.childID, From: childNode, To: parentNode, Type: "attachment_of"})
	}
	return nil
}

func addGraphNode(out *GraphSnapshot, known map[string]bool, node GraphNode) bool {
	if known[node.ID] {
		return true
	}
	if len(out.Nodes) == 256 {
		out.Truncated = true
		return false
	}
	out.Nodes = append(out.Nodes, node)
	known[node.ID] = true
	return true
}

func attachmentEvidenceMatches(raw sql.NullString, hash string) bool {
	if !raw.Valid || hash == "" {
		return false
	}
	var evidence struct {
		HeaderFields map[string]string `json:"header_fields"`
	}
	if json.Unmarshal([]byte(raw.String), &evidence) != nil {
		return false
	}
	return evidence.HeaderFields["file_hash"] == sha([]byte(strconv.Quote(hash)))
}
