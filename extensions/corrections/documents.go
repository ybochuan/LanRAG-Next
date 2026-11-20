package corrections

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// Filter narrows the caller's authorized knowledge base and presentation window.
type Filter struct {
	DocumentID, VersionID, Query, State, ApprovedFrom, ApprovedTo string
	Page, PageSize                                                int
	History                                                       bool
}

// Summary represents one correction document per exact source version.
type Summary struct {
	KnowledgeBaseID string    `json:"knowledge_base_id"`
	DocumentID      string    `json:"document_id"`
	VersionID       string    `json:"version_id"`
	DocumentTitle   string    `json:"document_title"`
	FileName        string    `json:"file_name"`
	ActiveCount     int       `json:"active_count"`
	LastApprovedAt  time.Time `json:"last_approved_at"`
}

// Page is the paginated management listing.
type Page struct {
	Items    []Summary `json:"items"`
	Total    int       `json:"total"`
	Page     int       `json:"page"`
	PageSize int       `json:"page_size"`
}

// Document is a permission-checked preview and download payload.
type Document struct {
	Summary
	Markdown string  `json:"markdown"`
	Items    []Entry `json:"items"`
}

type versionKey struct{ doc, version string }

func (s *Service) Documents(ctx context.Context, scope Scope, f Filter, titles func(context.Context, string, []string) (map[string]string, error)) (Page, error) {
	if f.Page == 0 {
		f.Page = 1
	}
	if f.PageSize == 0 {
		f.PageSize = 20
	}
	out := Page{Items: []Summary{}, Page: f.Page, PageSize: f.PageSize}
	if f.Page < 1 || f.Page > 10000 || f.PageSize < 1 || f.PageSize > 100 {
		return out, ErrInvalid
	}
	entries, err := s.filtered(ctx, scope, f)
	if err != nil {
		return out, err
	}
	groups := map[versionKey]int{}
	var ordered []Summary
	for _, e := range entries {
		key := versionKey{e.DocumentID, e.VersionID}
		index, exists := groups[key]
		if !exists {
			index = len(ordered)
			groups[key] = index
			ordered = append(ordered, summary(scope, e))
		}
		ordered[index].include(e)
	}
	slices.SortStableFunc(ordered, func(a, b Summary) int {
		return b.LastApprovedAt.Compare(a.LastApprovedAt)
	})
	out.Total = len(ordered)
	offset := (f.Page - 1) * f.PageSize
	if offset >= out.Total {
		return out, nil
	}
	out.Items = ordered[offset:min(offset+f.PageSize, out.Total)]
	applyTitles(ctx, scope, titles, out.Items)
	return out, nil
}

func (s *Service) Document(ctx context.Context, scope Scope, f Filter, titles func(context.Context, string, []string) (map[string]string, error)) (Document, error) {
	if f.DocumentID == "" || f.VersionID == "" {
		return Document{}, ErrInvalid
	}
	entries, err := s.filtered(ctx, scope, f)
	if err != nil {
		return Document{}, err
	}
	if len(entries) == 0 {
		return Document{}, ErrNotFound
	}
	header := []Summary{summary(scope, entries[0])}
	applyTitles(ctx, scope, titles, header)
	out := Document{Summary: header[0], Items: entries}
	for _, e := range entries {
		out.Summary.include(e)
	}
	out.Markdown = markdown(out.DocumentTitle, entries)
	return out, nil
}

func (s *Summary) include(e Entry) {
	if e.State == "active" {
		s.ActiveCount++
	}
	if e.ApprovedAt.After(s.LastApprovedAt) {
		s.LastApprovedAt = e.ApprovedAt
	}
}

func summary(scope Scope, e Entry) Summary {
	return Summary{KnowledgeBaseID: scope.KnowledgeBaseID, DocumentID: e.DocumentID, VersionID: e.VersionID, DocumentTitle: e.DocumentID, FileName: fileName(e.DocumentID), LastApprovedAt: e.ApprovedAt}
}

func applyTitles(ctx context.Context, scope Scope, titles func(context.Context, string, []string) (map[string]string, error), summaries []Summary) {
	if titles == nil || len(summaries) == 0 {
		return
	}
	ids := make([]string, 0, len(summaries))
	seen := map[string]bool{}
	for _, s := range summaries {
		if !seen[s.DocumentID] {
			ids = append(ids, s.DocumentID)
			seen[s.DocumentID] = true
		}
	}
	names, err := titles(ctx, scope.KnowledgeBaseID, ids)
	if err != nil {
		return
	}
	for i := range summaries {
		if title := strings.TrimSpace(names[summaries[i].DocumentID]); title != "" {
			summaries[i].DocumentTitle = title
			summaries[i].FileName = fileName(title)
		}
	}
}

func fileName(title string) string {
	title = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || strings.ContainsRune("\\/:*?\"<>|", r) {
			return '_'
		}
		return r
	}, title)
	return strings.TrimRight(title, ". ") + "反馈文档.md"
}

func dateBound(raw string, end bool) (time.Time, error) {
	if raw == "" {
		return time.Time{}, nil
	}
	if date, err := time.Parse(time.DateOnly, raw); err == nil {
		if end {
			date = date.AddDate(0, 0, 1).Add(-time.Nanosecond)
		}
		return date, nil
	}
	return time.Parse(time.RFC3339Nano, raw)
}

func (s *Service) filtered(ctx context.Context, scope Scope, f Filter) ([]Entry, error) {
	if s == nil || s.db == nil {
		return nil, ErrUnavailable
	}
	if scope.TenantID == 0 || !ValidID(scope.KnowledgeBaseID) || f.DocumentID != "" && !ValidID(f.DocumentID) || f.VersionID != "" && !ValidID(f.VersionID) || !ValidQuery(f.Query) {
		return nil, ErrInvalid
	}
	if f.State == "" {
		f.State = "active"
	}
	switch f.State {
	case "active":
	case "all", "withdrawn", "superseded", "inactive":
		if !f.History {
			return nil, ErrInvalid
		}
	default:
		return nil, ErrInvalid
	}
	from, err := dateBound(f.ApprovedFrom, false)
	if err != nil {
		return nil, ErrInvalid
	}
	to, err := dateBound(f.ApprovedTo, true)
	if err != nil || !to.IsZero() && to.Before(from) {
		return nil, ErrInvalid
	}
	var where string
	var args []any
	if f.DocumentID != "" {
		where += " AND p.document_id=?"
		args = append(args, f.DocumentID)
	}
	if f.VersionID != "" {
		where += " AND p.version_id=?"
		args = append(args, f.VersionID)
	}
	var rows []Entry
	err = s.db.View(ctx, func(tx *sql.Tx) error {
		var err error
		rows, err = readTx(ctx, tx, scope, s.now(), where, args...)
		return err
	})
	if err != nil {
		return nil, err
	}
	terms := strings.ToLower(strings.TrimSpace(f.Query))
	ordinals := map[versionKey]int{}
	out := []Entry{}
	for _, e := range rows {
		if f.State != "all" && e.State != f.State {
			continue
		}
		key := versionKey{e.DocumentID, e.VersionID}
		ordinals[key]++
		e.Ordinal = ordinals[key]
		if e.ApprovedAt.Before(from) || !to.IsZero() && e.ApprovedAt.After(to) {
			continue
		}
		if terms != "" && !strings.Contains(strings.ToLower(e.AppliesWhen+"\n"+e.Correction+"\n"+e.ID+"\n"+e.ChunkID), terms) {
			continue
		}
		out = append(out, e)
	}
	return out, nil
}

// markdown writes only approved fields and escapes Markdown and HTML markup.
func markdown(title string, entries []Entry) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", plain(title))
	for _, e := range entries {
		fmt.Fprintf(&b, "## %d. %s\n\n- 反馈编号：%s\n- 原文分块：%s\n- 适用条件：%s\n- 修正口径：%s\n- 生效时间：%s\n- 状态：%s\n\n", e.Ordinal, e.ApprovedAt.Format(time.DateOnly), plain(e.ID), plain(e.ChunkID), plain(e.AppliesWhen), plain(e.Correction), e.EffectiveAt.Format(time.RFC3339), e.State)
	}
	return b.String()
}

func plain(text string) string {
	text = html.EscapeString(strings.Join(strings.Fields(text), " "))
	return strings.NewReplacer("\\", "\\\\", "[", "\\[", "]", "\\]", "*", "\\*", "_", "\\_", "`", "\\`", "~", "\\~").Replace(text)
}

// Project reconciles per-version work from the committed snapshot. The
// periodic scan also repairs missing files and handles lifecycle/time changes.
func (s *Service) Project(ctx context.Context, rootPath string, scope Scope) error {
	type work struct {
		versionKey
		revision, rendered int64
		failed             bool
	}
	var jobs []work
	byVersion := map[versionKey][]Entry{}
	err := s.db.View(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT document_id,version_id,revision,rendered_revision,last_failure_at IS NOT NULL
   FROM ext_feedback_correction_projections WHERE tenant_id=? AND knowledge_base_id=?`, scopeArgs(scope)...)
		if err != nil {
			return err
		}
		for rows.Next() {
			var j work
			if err := rows.Scan(&j.doc, &j.version, &j.revision, &j.rendered, &j.failed); err != nil {
				rows.Close()
				return err
			}
			jobs = append(jobs, j)
		}
		err = rows.Err()
		rows.Close()
		if err != nil || len(jobs) == 0 {
			return err
		}
		entries, err := readTx(ctx, tx, scope, s.now(), activeOnly)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if e.State != "active" {
				continue
			}
			key := versionKey{e.DocumentID, e.VersionID}
			e.Ordinal = len(byVersion[key]) + 1
			byVersion[key] = append(byVersion[key], e)
		}
		archived, err := readTx(ctx, tx, scope, s.now(), archivedOnly)
		if err != nil {
			return err
		}
		for _, e := range archived {
			key := versionKey{e.DocumentID, e.VersionID}
			e.Ordinal = len(byVersion[key]) + 1
			byVersion[key] = append(byVersion[key], e)
		}
		return nil
	})
	if err != nil || len(jobs) == 0 {
		return err
	}
	if err = os.MkdirAll(rootPath, 0700); err != nil {
		return errors.Join(err, s.recordProjectionFailure(ctx, scope))
	}
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		return errors.Join(err, s.recordProjectionFailure(ctx, scope))
	}
	defer root.Close()
	var failures error
	for _, j := range jobs {
		if err := ctx.Err(); err != nil {
			return errors.Join(failures, err)
		}
		dir := filepath.Join(strconv.FormatUint(scope.TenantID, 10), pathID(scope.KnowledgeBaseID), pathID(j.doc), pathID(j.version))
		entries := byVersion[j.versionKey]
		title := "反馈文档"
		if len(entries) > 0 && entries[0].State == "inactive" {
			title = "反馈文档（历史版本）"
		}
		body := []byte(markdown(title, entries))
		if j.revision == j.rendered && !j.failed {
			if old, err := root.ReadFile(filepath.Join(dir, "feedback.md")); err == nil && bytes.Equal(old, body) {
				continue
			}
		}
		var writeErr error
		err := s.db.Write(ctx, func(tx *sql.Tx) error {
			key := append(scopeArgs(scope), j.doc, j.version)
			var current int64
			if err := tx.QueryRowContext(ctx, `SELECT revision FROM ext_feedback_correction_projections
    WHERE tenant_id=? AND knowledge_base_id=? AND document_id=? AND version_id=?`, key...).Scan(&current); err != nil {
				return err
			}
			if current != j.revision {
				return nil
			} // A review change committed after this snapshot.
			writeErr = writeProjection(root, dir, body)
			if writeErr != nil {
				_, err := tx.ExecContext(ctx, `UPDATE ext_feedback_correction_projections SET last_failure_at=?
     WHERE tenant_id=? AND knowledge_base_id=? AND document_id=? AND version_id=?`, append([]any{s.now().UTC().Format(time.RFC3339Nano)}, key...)...)
				return err
			}
			_, err := tx.ExecContext(ctx, `UPDATE ext_feedback_correction_projections SET rendered_revision=revision,last_failure_at=NULL
    WHERE tenant_id=? AND knowledge_base_id=? AND document_id=? AND version_id=?`, key...)
			return err
		})
		failures = errors.Join(failures, writeErr, err)
	}
	return failures
}

func (s *Service) recordProjectionFailure(ctx context.Context, scope Scope) error {
	return s.db.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE ext_feedback_correction_projections SET last_failure_at=? WHERE tenant_id=? AND knowledge_base_id=?`,
			append([]any{s.now().UTC().Format(time.RFC3339Nano)}, scopeArgs(scope)...)...)
		return err
	})
}

// Path components use reversible encoding outside the native ASCII ID alphabet.
func pathID(id string) string {
	if id != "" && strings.IndexFunc(id, func(r rune) bool { return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_') }) < 0 && !strings.Contains("|con|prn|aux|nul|com1|com2|com3|com4|com5|com6|com7|com8|com9|lpt1|lpt2|lpt3|lpt4|lpt5|lpt6|lpt7|lpt8|lpt9|", "|"+id+"|") {
		return id
	}
	return "~" + hex.EncodeToString([]byte(id))
}

func writeProjection(root *os.Root, dir string, body []byte) error {
	path := filepath.Join(dir, "feedback.md")
	old, err := root.ReadFile(path)
	if err == nil && bytes.Equal(old, body) {
		return nil
	}
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err = root.MkdirAll(dir, 0700); err != nil {
		return err
	}
	temp := filepath.Join(dir, ".feedback-"+strconv.FormatInt(time.Now().UnixNano(), 36)+".tmp")
	file, err := root.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer root.Remove(temp)
	_, err = file.Write(body)
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err = root.Rename(temp, path); err != nil {
		return err
	}
	return nil
}
