package lineage

import (
	"context"
	"database/sql"
	"encoding/json"
	"strconv"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/provenance"
)

// GraphNode describes registered metadata. State is the document's current lifecycle.
type GraphNode struct {
	ID                 string    `json:"id"`
	Type               string    `json:"type"`
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
		rows, err := tx.QueryContext(ctx, `SELECT n.node_id,n.node_type,
CASE WHEN n.node_type='document_version' THEN coalesce(v.document_id,n.native_id) ELSE '' END,
coalesce(v.family_id,''),coalesce(m.business_family_id,''),coalesce(v.observation_sequence,0),
coalesce(f.current_version_node_id=n.node_id,0),coalesce(c.state,''),coalesce(av.status,''),n.created_at
FROM ext_lineage_node n
LEFT JOIN ext_document_version v ON v.tenant_id=n.tenant_id AND v.knowledge_base_id=n.knowledge_base_id AND v.version_node_id=n.node_id
LEFT JOIN ext_document_version_family f ON f.tenant_id=v.tenant_id AND f.knowledge_base_id=v.knowledge_base_id AND f.family_id=v.family_id
LEFT JOIN ext_lifecycle_current c ON c.tenant_id=v.tenant_id AND c.knowledge_base_id=v.knowledge_base_id AND c.document_id=v.document_id
LEFT JOIN ext_document_availability av ON av.tenant_id=v.tenant_id AND av.knowledge_base_id=v.knowledge_base_id AND av.document_id=v.document_id
LEFT JOIN ext_business_family_projection_scopes p ON p.tenant_id=v.tenant_id AND p.knowledge_base_id=v.knowledge_base_id AND p.enabled=1
LEFT JOIN ext_business_document_family_members m ON m.tenant_id=p.tenant_id AND m.knowledge_base_id=p.knowledge_base_id AND m.document_id=v.document_id
WHERE n.tenant_id=? AND n.knowledge_base_id=?`+where+" ORDER BY n.created_at DESC,n.node_id LIMIT 257", params...)
		if err != nil {
			return err
		}
		for rows.Next() {
			var node GraphNode
			var created string
			if err := rows.Scan(&node.ID, &node.Type, &node.DocumentID, &node.FamilyID, &node.BusinessFamilyID, &node.Sequence, &node.Current, &node.State, &node.Availability, &created); err != nil {
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
		if mode == "relations" && len(ids) > 0 {
			if err := appendNavigationEdges(ctx, tx, s, string(body), &out); err != nil {
				return err
			}
			return appendAttachmentEdges(ctx, tx, s, string(body), &out)
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
	rows, err := tx.QueryContext(ctx, `SELECT 'relation:'||r.relation_id,r.source_version_id,r.target_version_id,r.edge_type
	FROM ext_document_relations r
	JOIN ext_document_version_family a ON a.tenant_id=r.tenant_id AND a.knowledge_base_id=r.knowledge_base_id
		AND a.document_id=r.document_id AND a.current_version_node_id=r.source_version_id
	JOIN ext_document_version_family b ON b.tenant_id=r.tenant_id AND b.knowledge_base_id=r.knowledge_base_id
		AND b.document_id=r.target_document_id AND b.current_version_node_id=r.target_version_id
	WHERE r.tenant_id=? AND r.knowledge_base_id=? AND r.status='active'
	AND r.edge_type IN ('cites','implements','template_for')
	AND r.source_version_id IN (SELECT value FROM json_each(?))
	AND r.target_version_id IN (SELECT value FROM json_each(?))
	ORDER BY r.relation_id LIMIT 513`, scopeArgs(s, ids, ids)...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var edge GraphEdge
		if err := rows.Scan(&edge.ID, &edge.From, &edge.To, &edge.Type); err != nil {
			return err
		}
		if len(out.Edges) == 512 {
			out.Truncated = true
			break
		}
		out.Edges = append(out.Edges, edge)
	}
	return rows.Err()
}

// appendAttachmentEdges projects stored ownership only onto current observed
// versions whose file fingerprints match the binding.
func appendAttachmentEdges(ctx context.Context, tx *sql.Tx, s provenance.Scope, ids string, out *GraphSnapshot) error {
	rows, err := tx.QueryContext(ctx, `SELECT a.attachment_id,c.current_version_node_id,p.current_version_node_id,
	a.attachment_file_hash,a.parent_file_hash,
	json_extract(ce.snapshot_json,'$.header_fields.file_hash'),
	json_extract(pe.snapshot_json,'$.header_fields.file_hash')
	FROM ext_attachment_links a
	JOIN ext_document_version_family c ON c.tenant_id=a.tenant_id AND c.knowledge_base_id=a.knowledge_base_id AND c.document_id=a.attachment_id
	JOIN ext_document_version_family p ON p.tenant_id=a.tenant_id AND p.knowledge_base_id=a.knowledge_base_id AND p.document_id=a.parent_document_id
	JOIN ext_source_snapshot_evidence ce ON ce.tenant_id=c.tenant_id AND ce.knowledge_base_id=c.knowledge_base_id AND ce.version_node_id=c.current_version_node_id
	JOIN ext_source_snapshot_evidence pe ON pe.tenant_id=p.tenant_id AND pe.knowledge_base_id=p.knowledge_base_id AND pe.version_node_id=p.current_version_node_id
	WHERE a.tenant_id=? AND a.knowledge_base_id=? AND a.removed_at IS NULL AND a.retired_at IS NULL
	AND NOT EXISTS (SELECT 1 FROM ext_attachment_delete_intents d WHERE d.tenant_id=a.tenant_id
	AND d.knowledge_base_id=a.knowledge_base_id AND d.document_id=a.parent_document_id)
	AND c.current_version_node_id IN (SELECT value FROM json_each(?))
	AND p.current_version_node_id IN (SELECT value FROM json_each(?))
	ORDER BY a.attachment_id`, scopeArgs(s, ids, ids)...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var childID, childHash, parentHash string
		var childEvidence, parentEvidence sql.NullString
		edge := GraphEdge{Type: "attachment_of"}
		if err := rows.Scan(&childID, &edge.From, &edge.To, &childHash, &parentHash, &childEvidence, &parentEvidence); err != nil {
			return err
		}
		if childEvidence.String != sha([]byte(strconv.Quote(childHash))) || parentEvidence.String != sha([]byte(strconv.Quote(parentHash))) {
			continue
		}
		if len(out.Edges) == 512 {
			out.Truncated = true
			break
		}
		edge.ID = "attachment:" + childID
		out.Edges = append(out.Edges, edge)
	}
	return rows.Err()
}
