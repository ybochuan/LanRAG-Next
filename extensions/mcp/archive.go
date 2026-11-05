package mcp

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"strings"

	"github.com/ybochuan/LanRAG-Next/extensions/store"
)

const archiveNotice = "【系统提示：以下内容来自历史归档文档，已失效，请在回答时提醒用户】"

type archivedChunk struct {
	DocumentID string `json:"document_id"`
	ChunkID    string `json:"chunk_id"`
	Title      string `json:"title"`
	State      string `json:"state"`
	Excerpt    string `json:"excerpt"`
}

func (s *Server) searchArchive(ctx context.Context, query string, scope nativeScope) ([]archivedChunk, error) {
	query = strings.TrimSpace(query)
	if query == "" || len(query) > 512 || s.DB == nil || s.NativeDBPath == "" || scope.Tenant == 0 || scope.Tenant != s.Tenant || len(scope.KBs) == 0 || len(scope.IDs)+len(scope.WholeKBs) == 0 {
		return nil, errors.New("archive search unavailable or unauthorized")
	}
	for _, kb := range scope.KBs {
		if !slices.Contains(s.KBs, kb) {
			return nil, errors.New("archive KB denied")
		}
	}
	kbs, _ := json.Marshal(scope.KBs)
	whole, _ := json.Marshal(scope.WholeKBs)
	ids, _ := json.Marshal(scope.IDs)
	hits := []archivedChunk{}
	err := s.DB.ViewNative(ctx, s.NativeDBPath, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `WITH matches AS (
SELECT c.document_id, ch.id, coalesce(k.title,'') AS title, c.state, ch.content, ch.chunk_index,
       instr(lower(ch.content),lower(?1)) AS hit, instr(lower(k.title),lower(?1)) AS title_hit
FROM ext_lifecycle_current c `+store.LifecycleEventJoinSQL+`
JOIN weknora.knowledges k ON k.id=c.document_id AND k.tenant_id=c.tenant_id AND k.knowledge_base_id=c.knowledge_base_id
JOIN weknora.chunks ch ON ch.knowledge_id=k.id AND ch.tenant_id=k.tenant_id AND ch.knowledge_base_id=k.knowledge_base_id
WHERE c.tenant_id=?2 AND c.state IN ('stale','archived') AND (`+store.LifecycleCurrentVerifiedSQL+`)
  AND c.knowledge_base_id IN (SELECT value FROM json_each(?3))
  AND (c.knowledge_base_id IN (SELECT value FROM json_each(?4)) OR c.document_id IN (SELECT value FROM json_each(?5)))
  AND k.deleted_at IS NULL AND k.parse_status='completed'
  AND ch.deleted_at IS NULL AND ch.is_enabled=1 AND ch.chunk_type='text'
)
SELECT document_id,id,title,state,substr(content,max(1,hit-200),1200) FROM matches
WHERE hit>0 OR title_hit>0 ORDER BY hit>0 DESC, document_id, chunk_index, id LIMIT 8`, query, strconv.FormatUint(s.Tenant, 10), string(kbs), string(whole), string(ids))
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var hit archivedChunk
			if err := rows.Scan(&hit.DocumentID, &hit.ChunkID, &hit.Title, &hit.State, &hit.Excerpt); err != nil {
				return err
			}
			hits = append(hits, hit)
		}
		return rows.Err()
	})
	return hits, err
}
