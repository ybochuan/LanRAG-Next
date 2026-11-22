package management

import (
	"context"
	"database/sql"
	"strings"

	"github.com/ybochuan/LanRAG-Next/extensions/store"
)

// nativeOverviewFrom makes native knowledges the document membership source.
// Extension lifecycle rows are joined as attributes; a document without a
// lifecycle row still appears as pending because it exists in the native list.
const nativeOverviewFrom = ` FROM weknora.knowledges k LEFT JOIN ext_lifecycle_current c ON c.tenant_id=k.tenant_id AND c.knowledge_base_id=k.knowledge_base_id AND c.document_id=k.id` + store.LifecycleEventJoinSQL

// nativeAttachmentParentStateSQL returns the verified lifecycle state of the
// attachment's current parent. It is used only to display the parent's state on
// an active attachment; the child's own lifecycle row and event stream stay
// unchanged.
func nativeAttachmentParentStateSQL() string {
	return `(SELECT pc.state
FROM ext_attachment_links l
JOIN ext_lifecycle_current pc ON pc.tenant_id=l.tenant_id AND pc.knowledge_base_id=l.knowledge_base_id AND pc.document_id=l.parent_document_id
JOIN ext_lifecycle_events pe ON pe.tenant_id=pc.tenant_id AND pe.knowledge_base_id=pc.knowledge_base_id AND pe.document_id=pc.document_id AND pe.event_id=pc.last_event_id
WHERE l.tenant_id=k.tenant_id AND l.knowledge_base_id=k.knowledge_base_id AND l.attachment_id=k.id
AND l.removed_at IS NULL AND l.retired_at IS NULL
AND pc.state=pe.to_state
AND NOT EXISTS (SELECT 1 FROM ext_lifecycle_events newer WHERE newer.tenant_id=pc.tenant_id AND newer.knowledge_base_id=pc.knowledge_base_id AND newer.document_id=pc.document_id AND newer.sequence>pe.sequence)
ORDER BY l.created_at DESC LIMIT 1)`
}

func nativeOverviewState() string {
	own := `COALESCE(CASE WHEN c.document_id IS NULL OR NOT (` + store.LifecycleCurrentVerifiedSQL + `) THEN 'pending' ELSE c.state END,'pending')`
	parent := nativeAttachmentParentStateSQL()
	return `CASE WHEN ` + parent + ` IS NOT NULL AND ` + parent + `<>'active' THEN ` + parent + ` WHEN (` + own + `)='active' THEN COALESCE(` + parent + `,` + own + `) ELSE ` + own + ` END`
}

func (s *Server) nativeOverviewCounts(ctx context.Context, tenant, kb string) (map[string]int, error) {
	counts := map[string]int{}
	err := s.db.ViewNative(ctx, s.nativeDB, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT state,count(*) FROM (SELECT `+nativeOverviewState()+` AS state`+nativeOverviewFrom+` WHERE k.tenant_id=? AND k.knowledge_base_id=? AND COALESCE(k.deleted_at,'')='') GROUP BY state`, tenant, kb)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var state string
			var n int
			if err := rows.Scan(&state, &n); err != nil {
				return err
			}
			counts[state] = n
		}
		return rows.Err()
	})
	return counts, err
}

func (s *Server) overviewPage(ctx context.Context, r Request, tenant string) ([]document, int, bool, error) {
	offset := (r.Page - 1) * r.PageSize
	read := func(tx *sql.Tx, query string, args ...any) ([]document, error) {
		rows, err := tx.QueryContext(ctx, query, args...)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		docs := []document{}
		for rows.Next() {
			var d document
			if err := rows.Scan(&d.DocumentID, &d.State, &d.LastEventID, &d.Title); err != nil {
				return nil, err
			}
			docs = append(docs, d)
		}
		return docs, rows.Err()
	}
	var docs []document
	var total int
	degraded := false
	if s.nativeDB != "" {
		err := s.db.ViewNative(ctx, s.nativeDB, func(tx *sql.Tx) error {
			where := ` WHERE k.tenant_id=? AND k.knowledge_base_id=? AND COALESCE(k.deleted_at,'')='' AND (?='' OR ` + nativeOverviewState() + `=? ) AND (?='' OR instr(lower(k.title), lower(?))>0 OR EXISTS (SELECT 1 FROM ext_attachment_semantics s WHERE s.tenant_id=k.tenant_id AND s.knowledge_base_id=k.knowledge_base_id AND s.attachment_id=k.id AND instr(lower(s.display_name), lower(?))>0) OR k.id IN (SELECT l.parent_document_id FROM ext_attachment_links l JOIN ext_attachment_semantics s ON s.tenant_id=l.tenant_id AND s.knowledge_base_id=l.knowledge_base_id AND s.attachment_id=l.attachment_id WHERE l.tenant_id=k.tenant_id AND l.knowledge_base_id=k.knowledge_base_id AND l.removed_at IS NULL AND l.retired_at IS NULL AND instr(lower(s.display_name), lower(?))>0))`
			params := []any{tenant, r.KnowledgeBaseID, r.State, r.State, r.Title, r.Title, r.Title, r.Title}
			if err := tx.QueryRowContext(ctx, `SELECT count(*)`+nativeOverviewFrom+where, params...).Scan(&total); err != nil {
				return err
			}
			var err error
			docs, err = read(tx, `SELECT k.id,`+nativeOverviewState()+`,COALESCE(c.last_event_id,''),COALESCE(k.title,'')`+nativeOverviewFrom+where+` ORDER BY k.id LIMIT ? OFFSET ?`, append(params, r.PageSize, offset)...)
			return err
		})
		if err == nil {
			return docs, total, false, nil
		}
		if !store.SQLiteUnavailable(err) {
			return docs, total, false, err
		}
		degraded = true
	}
	// Native unavailable: keep the last verified extension projection, but mark
	// the response degraded and let the normal title reader fill names.
	const extOnly = ` FROM ext_lifecycle_current c` + store.LifecycleEventJoinSQL + ` WHERE c.tenant_id=? AND c.knowledge_base_id=? AND (?='' OR c.state=?) AND ` + store.LifecycleCurrentVerifiedSQL
	err := s.db.View(ctx, func(tx *sql.Tx) error {
		args := []any{tenant, r.KnowledgeBaseID, r.State, r.State}
		if r.Title != "" {
			var err error
			docs, total, err = s.titleFallbackPage(ctx, tx, extOnly, args, r)
			return err
		}
		query := `SELECT c.document_id,c.state,c.last_event_id,''` + extOnly + ` ORDER BY c.document_id`
		if err := tx.QueryRowContext(ctx, `SELECT count(*)`+extOnly, args...).Scan(&total); err != nil {
			return err
		}
		var err error
		docs, err = read(tx, query+` LIMIT ? OFFSET ?`, append(args, r.PageSize, offset)...)
		return err
	})
	if err != nil || len(docs) == 0 || r.Title != "" {
		return docs, total, degraded, err
	}
	if s.titles == nil {
		return docs, total, degraded, nil
	}
	ids := make([]string, 0, len(docs))
	for _, d := range docs {
		ids = append(ids, d.DocumentID)
	}
	titles, err := s.titles(ctx, r.KnowledgeBaseID, ids)
	if err != nil {
		return nil, 0, degraded, err
	}
	for i := range docs {
		docs[i].Title = titles[docs[i].DocumentID]
	}
	return docs, total, degraded, nil
}

// titleFallbackPage scans the scoped current rows in bounded chunks so a
// title filter cannot materialize an unbounded document list in memory.
func (s *Server) titleFallbackPage(ctx context.Context, tx *sql.Tx, from string, args []any, r Request) ([]document, int, error) {
	const chunk = 256
	if s.titles == nil {
		var exists bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1`+from+`)`, args...).Scan(&exists); err != nil {
			return nil, 0, err
		}
		if !exists {
			return []document{}, 0, nil
		}
		return nil, 0, errDatabase
	}
	rows, err := tx.QueryContext(ctx, `SELECT c.document_id,c.state,c.last_event_id,''`+from+` ORDER BY c.document_id`, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	needle := sqliteLower(r.Title)
	offset := (r.Page - 1) * r.PageSize
	page := make([]document, 0, r.PageSize)
	batch := make([]document, 0, chunk)
	total := 0
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		ids := make([]string, len(batch))
		for i := range batch {
			ids[i] = batch[i].DocumentID
		}
		titles, err := s.titles(ctx, r.KnowledgeBaseID, ids)
		if err != nil {
			return err
		}
		for _, d := range batch {
			title := titles[d.DocumentID]
			if !strings.Contains(sqliteLower(title), needle) {
				continue
			}
			if total >= offset && len(page) < r.PageSize {
				d.Title = title
				page = append(page, d)
			}
			total++
		}
		batch = batch[:0]
		return nil
	}
	for rows.Next() {
		var d document
		if err := rows.Scan(&d.DocumentID, &d.State, &d.LastEventID, &d.Title); err != nil {
			return nil, 0, err
		}
		batch = append(batch, d)
		if len(batch) == chunk {
			if err := flush(); err != nil {
				return nil, 0, err
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	if err := flush(); err != nil {
		return nil, 0, err
	}
	return page, total, nil
}

// Match SQLite's built-in lower() so results stay stable on the fallback path.
func sqliteLower(value string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'A' && r <= 'Z' {
			return r + ('a' - 'A')
		}
		return r
	}, value)
}
