// Package mcp exposes AgentRAG's read-only related-document tool.
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
	"strings"

	"github.com/ybochuan/LanRAG-Next/extensions/contract"
	"github.com/ybochuan/LanRAG-Next/extensions/retrieval"
	"github.com/ybochuan/LanRAG-Next/extensions/store"
	"github.com/ybochuan/LanRAG-Next/extensions/weknora"
)

// Server uses the worker's existing scoped API client and lifecycle policy.
type Server struct {
	DB     *store.DB
	Client *weknora.Client
	Policy *retrieval.Policy
	Key    string
	Tenant uint64
	KBs    []string
}
type nativeScope struct {
	Tenant   uint64   `json:"tenant_id"`
	KBs      []string `json:"knowledge_base_ids"`
	IDs      []string `json:"knowledge_ids"`
	WholeKBs []string `json:"whole_kb_ids"`
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
	authorized := false
	for _, doc := range scope.IDs {
		authorized = authorized || doc == id
	}
	for _, whole := range scope.WholeKBs {
		authorized = authorized || whole == kb
	}
	if !authorized {
		return nil, errors.New("document outside native scope")
	}
	req := contract.BeforeRetrieveRequest{Context: contract.Context{RequestID: "query-related", SessionID: "mcp", Mode: "agent", TenantID: s.Tenant, KnowledgeBaseIDs: []string{kb}, KnowledgeIDs: []string{id}}}
	excluded, e := s.Policy.ExcludedDocumentIDs(ctx, req)
	if e != nil || len(excluded) > 0 {
		return nil, errors.New("document inactive")
	}
	ids := []string{}
	e = s.DB.View(ctx, func(tx *sql.Tx) error {
		// supplements edges point from the notice version to the parent version.
		rows, e := tx.QueryContext(ctx, `SELECT DISTINCT child.document_id FROM ext_lineage_edge edge
   JOIN ext_document_version_family parent ON parent.tenant_id=edge.tenant_id AND parent.knowledge_base_id=edge.knowledge_base_id AND parent.current_version_node_id=edge.to_node_id
   JOIN ext_document_version_family child ON child.tenant_id=edge.tenant_id AND child.knowledge_base_id=edge.knowledge_base_id AND child.current_version_node_id=edge.from_node_id
   JOIN ext_supplement_edge_facts facts ON facts.tenant_id=edge.tenant_id AND facts.knowledge_base_id=edge.knowledge_base_id AND facts.edge_id=edge.edge_id
   JOIN ext_supplement_current current ON current.tenant_id=facts.tenant_id AND current.knowledge_base_id=facts.knowledge_base_id AND current.document_id=facts.document_id AND current.declaration_id=facts.declaration_id
   WHERE edge.tenant_id=? AND edge.knowledge_base_id=? AND parent.document_id=? AND edge.edge_type='supplements'`, fmt.Sprint(s.Tenant), kb, id)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var doc string
			if e = rows.Scan(&doc); e != nil {
				return e
			}
			ids = append(ids, doc)
		}
		return rows.Err()
	})
	if e != nil {
		return nil, e
	}
	notices := []contract.RelatedAttachment{}
	if len(ids) > 0 {
		req.KnowledgeIDs = ids
		blocked, e := s.Policy.ExcludedDocumentIDs(ctx, req)
		if e != nil {
			return nil, e
		}
		deny := map[string]bool{}
		for _, v := range blocked {
			deny[v] = true
		}
		found, e := s.Client.AttachmentDocuments(ctx, []string{kb}, ids)
		if e != nil {
			return nil, e
		}
		for _, d := range found {
			if !deny[d.ID] {
				notices = append(notices, contract.RelatedAttachment{KnowledgeID: d.ID, KnowledgeBaseID: kb, ParentDocumentID: id, FileName: d.FileName, DownloadURL: "/api/v1/knowledge/" + url.PathEscape(d.ID) + "/download"})
			}
		}
	}
	req.KnowledgeIDs = []string{id}
	links, e := s.Policy.AttachmentLinks(ctx, req)
	if e != nil {
		return nil, e
	}
	attachments := links[id]
	if attachments == nil {
		attachments = []contract.RelatedAttachment{}
	}
	all := append(append([]contract.RelatedAttachment{}, notices...), attachments...)
	return map[string]any{"document_id": id, "supplements": notices, "attachments": attachments, "related_attachments": all}, nil
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
		result = map[string]any{"tools": []any{map[string]any{"name": "query_related_docs", "description": "查询制度的有效补充通知和附件下载链接。先从已授权检索结果获取document_id。", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{"document_id": map[string]string{"type": "string"}}, "required": []string{"document_id"}, "additionalProperties": false}, "annotations": map[string]bool{"readOnlyHint": true, "destructiveHint": false, "openWorldHint": false}}}}
	case "tools/call":
		var p struct {
			Name string `json:"name"`
			Args struct {
				ID    string      `json:"document_id"`
				Scope nativeScope `json:"_native_scope"`
			} `json:"arguments"`
		}
		json.Unmarshal(in.Params, &p)
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
