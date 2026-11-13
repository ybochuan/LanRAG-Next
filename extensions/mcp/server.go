// Package mcp exposes AgentRAG's scoped read-only Agent tools.
package mcp

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/ybochuan/LanRAG-Next/extensions/contract"
	"github.com/ybochuan/LanRAG-Next/extensions/retrieval"
	"github.com/ybochuan/LanRAG-Next/extensions/store"
	"github.com/ybochuan/LanRAG-Next/extensions/weknora"
)

// Server uses the worker's existing scoped API client and lifecycle policy.
type Server struct {
	DB           *store.DB
	Client       *weknora.Client
	Policy       *retrieval.Policy
	Key          string
	Tenant       uint64
	KBs          []string
	NativeDBPath string // Lite database; required for archived chunk reads.
}
type nativeScope struct {
	Tenant   uint64   `json:"tenant_id"`
	KBs      []string `json:"knowledge_base_ids"`
	IDs      []string `json:"knowledge_ids"`
	WholeKBs []string `json:"whole_kb_ids"`
}

type relatedDocument struct {
	KnowledgeBaseID string  `json:"knowledge_base_id"`
	DocumentID      string  `json:"document_id"`
	EdgeType        string  `json:"edge_type"`
	Confidence      float64 `json:"confidence"`
	Direction       string  `json:"direction"`
}

func relatedDocumentKey(kb, id string) string { return kb + "\x00" + id }

func (scope nativeScope) allows(kb, id string) bool {
	return slices.Contains(scope.KBs, kb) && (slices.Contains(scope.IDs, id) || slices.Contains(scope.WholeKBs, kb))
}

func (s *Server) query(ctx context.Context, id string, scope nativeScope) (map[string]any, error) {
	if !weknora.ValidID(id) || scope.Tenant != s.Tenant || len(scope.KBs) == 0 {
		return nil, errors.New("invalid scope")
	}
	allowedKB := map[string]bool{}
	for _, kb := range s.KBs {
		allowedKB[kb] = true
	}
	for _, kb := range scope.KBs {
		if !allowedKB[kb] {
			return nil, errors.New("KB denied")
		}
	}
	docs, e := s.Client.AttachmentDocuments(ctx, scope.KBs, []string{id})
	if e != nil || len(docs) != 1 {
		return nil, errors.New("document unavailable")
	}
	kb := docs[0].KnowledgeBaseID
	if !scope.allows(kb, id) {
		return nil, errors.New("document outside native scope")
	}
	req := contract.BeforeRetrieveRequest{Context: contract.Context{RequestID: "query-related", SessionID: "mcp", Mode: "agent", TenantID: s.Tenant, KnowledgeBaseIDs: []string{kb}, KnowledgeIDs: []string{id}}}
	excluded, e := s.Policy.ExcludedDocumentIDs(ctx, req)
	if e != nil || len(excluded) > 0 {
		return nil, errors.New("document inactive")
	}
	idsByKB := map[string][]string{}
	e = s.DB.View(ctx, func(tx *sql.Tx) error {
		// supplements edges point from the notice version to the parent version.
		rows, e := tx.QueryContext(ctx, `SELECT child.knowledge_base_id,child.document_id FROM ext_lineage_edge edge
   JOIN ext_document_version_family parent ON parent.tenant_id=edge.tenant_id AND parent.knowledge_base_id=edge.knowledge_base_id AND parent.current_version_node_id=edge.to_node_id
   JOIN ext_document_version_family child ON child.tenant_id=edge.tenant_id AND child.knowledge_base_id=edge.knowledge_base_id AND child.current_version_node_id=edge.from_node_id
   JOIN ext_supplement_edge_facts facts ON facts.tenant_id=edge.tenant_id AND facts.knowledge_base_id=edge.knowledge_base_id AND facts.edge_id=edge.edge_id
   JOIN ext_supplement_current current ON current.tenant_id=facts.tenant_id AND current.knowledge_base_id=facts.knowledge_base_id AND current.document_id=facts.document_id AND current.declaration_id=facts.declaration_id
   WHERE edge.tenant_id=? AND edge.knowledge_base_id=? AND parent.document_id=? AND edge.edge_type='supplements'
   UNION ALL
   SELECT cp.knowledge_base_id,cp.document_id FROM ext_supplement_cross_parents cp
   JOIN ext_supplement_current current ON current.tenant_id=cp.tenant_id AND current.knowledge_base_id=cp.knowledge_base_id AND current.document_id=cp.document_id AND current.declaration_id=cp.declaration_id
   JOIN ext_supplement_declarations declaration ON declaration.tenant_id=current.tenant_id AND declaration.knowledge_base_id=current.knowledge_base_id AND declaration.document_id=current.document_id AND declaration.declaration_id=current.declaration_id AND declaration.revision=current.revision AND declaration.status='active'
   JOIN ext_document_version_family child ON child.tenant_id=cp.tenant_id AND child.knowledge_base_id=cp.knowledge_base_id AND child.document_id=cp.document_id AND child.current_version_node_id=declaration.child_version_node_id
   JOIN ext_document_version_family parent ON parent.tenant_id=cp.tenant_id AND parent.knowledge_base_id=cp.parent_knowledge_base_id AND parent.document_id=cp.parent_document_id AND parent.current_version_node_id=cp.parent_version_node_id
   WHERE cp.tenant_id=? AND cp.parent_knowledge_base_id=? AND cp.parent_document_id=? AND cp.parent_version_node_id=?`,
			fmt.Sprint(s.Tenant), kb, id,
			fmt.Sprint(s.Tenant), kb, id, id)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var childKB, doc string
			if e = rows.Scan(&childKB, &doc); e != nil {
				return e
			}
			if scope.allows(childKB, doc) {
				idsByKB[childKB] = append(idsByKB[childKB], doc)
			}
		}
		return rows.Err()
	})
	if e != nil {
		return nil, e
	}
	notices := []contract.RelatedAttachment{}
	for childKB, ids := range idsByKB {
		req.KnowledgeBaseIDs = []string{childKB}
		req.KnowledgeIDs = ids
		blocked, e := s.Policy.ExcludedDocumentIDs(ctx, req)
		if e != nil {
			return nil, e
		}
		deny := map[string]bool{}
		for _, v := range blocked {
			deny[v] = true
		}
		found, e := s.Client.AttachmentDocuments(ctx, []string{childKB}, ids)
		if e != nil {
			return nil, e
		}
		for _, d := range found {
			if !deny[d.ID] {
				notices = append(notices, contract.RelatedAttachment{KnowledgeID: d.ID, KnowledgeBaseID: childKB, ParentDocumentID: id, FileName: d.FileName, DownloadURL: "/api/v1/knowledge/" + url.PathEscape(d.ID) + "/download"})
			}
		}
	}
	req.KnowledgeBaseIDs = []string{kb}
	req.KnowledgeIDs = []string{id}
	links, e := s.Policy.AttachmentLinks(ctx, req)
	if e != nil {
		return nil, e
	}
	attachments := []contract.RelatedAttachment{}
	var inherited []string
	for _, item := range links[id] {
		if !scope.allows(item.KnowledgeBaseID, item.KnowledgeID) && item.KnowledgeBaseID == kb && item.ParentDocumentID == id {
			inherited = append(inherited, item.KnowledgeID)
		}
	}
	var owners map[string]store.AttachmentOwnership
	if len(inherited) > 0 {
		if e = s.DB.View(ctx, func(tx *sql.Tx) error {
			owners, e = store.AttachmentOwnershipsTx(ctx, tx, s.Tenant, inherited)
			return e
		}); e != nil {
			return nil, e
		}
	}
	for _, item := range links[id] {
		owner, owned := owners[item.KnowledgeID]
		if scope.allows(item.KnowledgeBaseID, item.KnowledgeID) ||
			(owned && !owner.Historical && item.KnowledgeBaseID == kb && owner.KnowledgeBaseID == kb && item.ParentDocumentID == id && owner.ParentDocumentID == id) {
			if !scope.allows(item.KnowledgeBaseID, item.ParentDocumentID) {
				// Selecting a child authorizes its link, but not parent-derived text.
				item = contract.RelatedAttachment{KnowledgeID: item.KnowledgeID, KnowledgeBaseID: item.KnowledgeBaseID,
					FileName: item.FileName, DownloadURL: item.DownloadURL}
			}
			attachments = append(attachments, item)
		}
	}
	all := append(append([]contract.RelatedAttachment{}, notices...), attachments...)
	relations, e := s.relatedDocuments(ctx, kb, id, scope)
	if e != nil {
		return nil, e
	}
	return map[string]any{"document_id": id, "supplements": notices, "relations": relations, "attachments": attachments, "related_attachments": all}, nil
}

// relatedDocuments returns version-pinned relationship metadata only. Target
// documents must be covered by the caller's native scope and current lifecycle.
func (s *Server) relatedDocuments(ctx context.Context, kb, id string, scope nativeScope) ([]relatedDocument, error) {
	candidates := []relatedDocument{}
	err := s.DB.View(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT r.target_knowledge_base_id,r.target_document_id,r.edge_type,r.confidence,'outgoing'
			FROM ext_document_relations r
			WHERE r.tenant_id=? AND r.knowledge_base_id=? AND r.document_id=? AND r.status='active'
			  AND (r.edge_type<>'related_to' OR r.confidence>=0.7 OR EXISTS(
			    SELECT 1 FROM ext_family_suggestions s
			    WHERE s.tenant_id=r.tenant_id AND s.knowledge_base_id=r.knowledge_base_id
			      AND s.applied_id=r.relation_id AND s.edge_type='related_to' AND s.status='confirmed'
			      AND s.source_version_id=r.source_version_id AND s.target_version_id=r.target_version_id))
			  AND EXISTS (SELECT 1 FROM ext_document_version_family f
			    WHERE f.tenant_id=r.tenant_id AND f.knowledge_base_id=r.knowledge_base_id AND f.document_id=r.document_id AND f.current_version_node_id=r.source_version_id)
			  AND EXISTS (SELECT 1 FROM ext_document_version_family t
			    WHERE t.tenant_id=r.tenant_id AND t.knowledge_base_id=r.target_knowledge_base_id AND t.document_id=r.target_document_id AND t.current_version_node_id=r.target_version_id)
			UNION ALL
			SELECT r.knowledge_base_id,r.document_id,r.edge_type,r.confidence,'incoming'
			FROM ext_document_relations r
			WHERE r.tenant_id=? AND r.target_knowledge_base_id=? AND r.target_document_id=? AND r.status='active'
			  AND (r.edge_type<>'related_to' OR r.confidence>=0.7 OR EXISTS(
			    SELECT 1 FROM ext_family_suggestions s
			    WHERE s.tenant_id=r.tenant_id AND s.knowledge_base_id=r.knowledge_base_id
			      AND s.applied_id=r.relation_id AND s.edge_type='related_to' AND s.status='confirmed'
			      AND s.source_version_id=r.source_version_id AND s.target_version_id=r.target_version_id))
			  AND EXISTS (SELECT 1 FROM ext_document_version_family f
			    WHERE f.tenant_id=r.tenant_id AND f.knowledge_base_id=r.knowledge_base_id AND f.document_id=r.document_id AND f.current_version_node_id=r.source_version_id)
			  AND EXISTS (SELECT 1 FROM ext_document_version_family t
			    WHERE t.tenant_id=r.tenant_id AND t.knowledge_base_id=r.target_knowledge_base_id AND t.document_id=r.target_document_id AND t.current_version_node_id=r.target_version_id)
			LIMIT 101`, fmt.Sprint(s.Tenant), kb, id, fmt.Sprint(s.Tenant), kb, id)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var row relatedDocument
			if err = rows.Scan(&row.KnowledgeBaseID, &row.DocumentID, &row.EdgeType, &row.Confidence, &row.Direction); err != nil {
				return err
			}
			if scope.allows(row.KnowledgeBaseID, row.DocumentID) {
				candidates = append(candidates, row)
			}
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	if len(candidates) > 100 {
		return nil, errors.New("relation result too large")
	}
	byKB := map[string][]string{}
	seen := map[string]bool{}
	for _, row := range candidates {
		key := relatedDocumentKey(row.KnowledgeBaseID, row.DocumentID)
		if !seen[key] {
			seen[key] = true
			byKB[row.KnowledgeBaseID] = append(byKB[row.KnowledgeBaseID], row.DocumentID)
		}
	}
	blocked := map[string]bool{}
	for targetKB, ids := range byKB {
		req := contract.BeforeRetrieveRequest{Context: contract.Context{RequestID: "query-related-relation", SessionID: "mcp", Mode: "agent", TenantID: s.Tenant, KnowledgeBaseIDs: []string{targetKB}, KnowledgeIDs: ids}}
		excluded, err := s.Policy.ExcludedDocumentIDs(ctx, req)
		if err != nil {
			return nil, err
		}
		for _, doc := range excluded {
			blocked[relatedDocumentKey(targetKB, doc)] = true
		}
	}
	out := []relatedDocument{}
	for _, row := range candidates {
		if !blocked[relatedDocumentKey(row.KnowledgeBaseID, row.DocumentID)] {
			out = append(out, row)
		}
	}
	return out, nil
}

// ServeHTTP is a stateless Streamable HTTP JSON-RPC endpoint (JSON responses).
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if len(s.Key) < 32 || subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+s.Key)) != 1 {
		http.Error(w, "unauthorized", 401)
		return
	}
	if r.Method != "POST" {
		w.Header().Set("Allow", "POST")
		w.WriteHeader(405)
		return
	}
	var in struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Method  string          `json:"method"`
		Params  json.RawMessage `json:"params"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&in) != nil || in.JSONRPC != "2.0" {
		http.Error(w, "invalid request", 400)
		return
	}
	if len(in.ID) == 0 && strings.HasPrefix(in.Method, "notifications/") {
		w.WriteHeader(202)
		return
	}
	var result any
	var rpcError any
	switch in.Method {
	case "initialize":
		var p struct {
			Version string `json:"protocolVersion"`
		}
		json.Unmarshal(in.Params, &p)
		if p.Version != "2024-11-05" && p.Version != "2025-03-26" && p.Version != "2025-06-18" {
			p.Version = "2025-03-26"
		}
		result = map[string]any{"protocolVersion": p.Version, "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]string{"name": "AgentRAG", "version": "1.0"}}
	case "ping":
		result = map[string]any{}
	case "tools/list":
		tools := []any{readTool("query_related_docs", "查询制度的有效补充通知、确认关联和附件下载链接。先从已授权检索结果获取document_id。", "document_id")}
		if s.NativeDBPath != "" {
			tools = append(tools, readTool("search_archived_documents", "仅当用户明确要求查阅过期或归档文档时，用短关键词 query 查询授权范围内的历史条款；必须提醒用户结果已失效。", "query"))
		}
		result = map[string]any{"tools": tools}
	case "tools/call":
		var p struct {
			Name string `json:"name"`
			Args struct {
				ID    string      `json:"document_id"`
				Query string      `json:"query"`
				Scope nativeScope `json:"_native_scope"`
			} `json:"arguments"`
		}
		json.Unmarshal(in.Params, &p)
		if p.Name == "search_archived_documents" {
			hits, err := s.searchArchive(r.Context(), p.Args.Query, p.Args.Scope)
			if err != nil {
				result = map[string]any{"isError": true, "content": []any{map[string]string{"type": "text", "text": "历史文档不可用：请检查查询词、授权范围及 Lite 数据库"}}}
				break
			}
			raw, _ := json.Marshal(hits)
			result = map[string]any{"content": []any{map[string]string{"type": "text", "text": archiveNotice + "\n" + string(raw)}}}
			break
		}
		if p.Name != "query_related_docs" {
			rpcError = map[string]any{"code": -32601, "message": "unknown tool"}
			break
		}
		data, e := s.query(r.Context(), p.Args.ID, p.Args.Scope)
		if e != nil {
			result = map[string]any{"isError": true, "content": []any{map[string]string{"type": "text", "text": "关联文档不可用：请检查授权范围及生命周期"}}}
			break
		}
		raw, _ := json.Marshal(data)
		result = map[string]any{"structuredContent": data, "content": []any{map[string]string{"type": "text", "text": string(raw)}}}
	default:
		rpcError = map[string]any{"code": -32601, "message": "method not found"}
	}
	w.Header().Set("Content-Type", "application/json")
	out := map[string]any{"jsonrpc": "2.0", "id": in.ID}
	if rpcError != nil {
		out["error"] = rpcError
	} else {
		out["result"] = result
	}
	json.NewEncoder(w).Encode(out)
}

func readTool(name, description, argument string) map[string]any {
	return map[string]any{
		"name": name, "description": description,
		"inputSchema": map[string]any{"type": "object", "properties": map[string]any{argument: map[string]string{"type": "string"}}, "required": []string{argument}, "additionalProperties": false},
		"annotations": map[string]bool{"readOnlyHint": true, "destructiveHint": false, "openWorldHint": false},
	}
}
