package management

import (
	"context"
	"database/sql"
	"strings"

	"github.com/ybochuan/LanRAG-Next/extensions/store"
)

func (s *Server) overviewPage(ctx context.Context, r Request, tenant string) ([]document, int, bool, error) {
	const scope = ` WHERE c.tenant_id=? AND c.knowledge_base_id=? AND (?='' OR c.state=?) AND ` + store.LifecycleCurrentVerifiedSQL
	args := []any{tenant, r.KnowledgeBaseID, r.State, r.State}
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
			const from = ` FROM ext_lifecycle_current c LEFT JOIN weknora.knowledges k
ON k.id=c.document_id AND k.tenant_id=c.tenant_id AND k.knowledge_base_id=c.knowledge_base_id AND k.deleted_at IS NULL` + store.LifecycleEventJoinSQL + scope + ` AND (?='' OR instr(lower(k.title), lower(?))>0)`
			params := append(args, r.Title, r.Title)
			if err := tx.QueryRowContext(ctx, "SELECT count(*)"+from, params...).Scan(&total); err != nil {
				return err
			}
			var err error
			docs, err = read(tx, `SELECT c.document_id,c.state,c.last_event_id,coalesce(k.title,'')`+from+` ORDER BY c.document_id LIMIT ? OFFSET ?`, append(params, r.PageSize, offset)...)
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
	// Release the attached connection before calling Core. Fetch only the visible
	// page unless a title filter requires matching the entire scoped list first.
	const extOnly = ` FROM ext_lifecycle_current c` + store.LifecycleEventJoinSQL + scope
	err := s.db.View(ctx, func(tx *sql.Tx) error {
		if r.Title != "" {
			var err error
			docs, total, err = s.titleFallbackPage(ctx, tx, extOnly, args, r)
			return err
		}
		query := `SELECT c.document_id,c.state,c.last_event_id,''` + extOnly + ` ORDER BY c.document_id`
		if err := tx.QueryRowContext(ctx, `SELECT count(*)`+extOnly, args...).Scan(&total); err != nil {
			return err
		}
		query += ` LIMIT ? OFFSET ?`
		var err error
		docs, err = read(tx, query, append(args, r.PageSize, offset)...)
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
		// Preserve the previous behavior for empty scopes: only a non-empty
		// title-filtered fallback requires the native title reader.
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
