package management

import (
	"context"
	"database/sql"
	"strings"

	"github.com/ybochuan/LanRAG-Next/extensions/store"
)

func (s *Server) overviewPage(ctx context.Context, r Request, tenant string) ([]document, int, error) {
	const scope = ` WHERE c.tenant_id=? AND c.knowledge_base_id=? AND (?='' OR c.state=?)`
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
	if s.nativeDB != "" {
		err := s.db.ViewNative(ctx, s.nativeDB, func(tx *sql.Tx) error {
			const from = ` FROM ext_lifecycle_current c LEFT JOIN weknora.knowledges k
ON k.id=c.document_id AND k.tenant_id=c.tenant_id AND k.knowledge_base_id=c.knowledge_base_id AND k.deleted_at IS NULL` + scope + ` AND (?='' OR instr(lower(k.title), lower(?))>0)`
			params := append(args, r.Title, r.Title)
			if err := tx.QueryRowContext(ctx, "SELECT count(*)"+from, params...).Scan(&total); err != nil {
				return err
			}
			var err error
			docs, err = read(tx, `SELECT c.document_id,c.state,c.last_event_id,coalesce(k.title,'')`+from+` ORDER BY c.document_id LIMIT ? OFFSET ?`, append(params, r.PageSize, offset)...)
			return err
		})
		if !store.SQLiteBusy(err) {
			return docs, total, err
		}
	}
	// Release the attached connection before calling Core. Fetch only the visible
	// page unless a title filter requires matching the entire scoped list first.
	const extOnly = ` FROM ext_lifecycle_current c` + scope
	err := s.db.View(ctx, func(tx *sql.Tx) error {
		query := `SELECT c.document_id,c.state,c.last_event_id,''` + extOnly + ` ORDER BY c.document_id`
		params := args
		if r.Title == "" {
			if err := tx.QueryRowContext(ctx, `SELECT count(*)`+extOnly, args...).Scan(&total); err != nil {
				return err
			}
			query += ` LIMIT ? OFFSET ?`
			params = append(args, r.PageSize, offset)
		}
		var err error
		docs, err = read(tx, query, params...)
		return err
	})
	if err != nil || len(docs) == 0 {
		return docs, total, err
	}
	if s.titles == nil {
		if r.Title != "" {
			return nil, 0, errDatabase
		}
		return docs, total, nil
	}
	ids := make([]string, 0, len(docs))
	for _, d := range docs {
		ids = append(ids, d.DocumentID)
	}
	titles, err := s.titles(ctx, r.KnowledgeBaseID, ids)
	if err != nil {
		return nil, 0, err
	}
	matched := docs[:0]
	needle := sqliteLower(r.Title)
	for _, d := range docs {
		d.Title = titles[d.DocumentID]
		if needle == "" || strings.Contains(sqliteLower(d.Title), needle) {
			matched = append(matched, d)
		}
	}
	if r.Title == "" {
		return matched, total, nil
	}
	total = len(matched)
	offset = min(offset, total)
	return matched[offset:min(offset+r.PageSize, total)], total, nil
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
