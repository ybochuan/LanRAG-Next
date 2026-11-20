// Package corrections reads and projects reviewed facts anchored to native sources.
package corrections

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/ybochuan/LanRAG-Next/extensions/dependencies"
	"github.com/ybochuan/LanRAG-Next/extensions/store"
	"github.com/ybochuan/LanRAG-Next/extensions/weknora"
)

var (
	ErrInvalid     = errors.New("invalid correction request")
	ErrNotFound    = errors.New("correction document not found")
	ErrUnavailable = errors.New("correction service unavailable")
)

// Scope identifies one authorized tenant and knowledge base.
type Scope struct {
	TenantID        uint64
	KnowledgeBaseID string
}

// Identity pins a correction and its review revision to an exact original chunk.
type Identity struct {
	ID         string `json:"id"`
	Revision   int64  `json:"revision"`
	DocumentID string `json:"document_id"`
	VersionID  string `json:"version_id"`
	ChunkID    string `json:"chunk_id"`
}

// Entry contains only the approved wording and public source metadata.
type Entry struct {
	Identity
	AppliesWhen string    `json:"applies_when"`
	Correction  string    `json:"correction"`
	State       string    `json:"state"`
	ApprovedAt  time.Time `json:"approved_at"`
	EffectiveAt time.Time `json:"effective_at"`
	Ordinal     int       `json:"ordinal"`
	contentHash string
}

// Source identifies an original retrieval candidate without correction prose.
type Source struct {
	DocumentID  string `json:"document_id"`
	ChunkID     string `json:"chunk_id"`
	ContentHash string `json:"content_hash"`
}

// Service reads the extension truth without consulting generated Markdown.
type Service struct {
	db  *store.DB
	now func() time.Time
}

func New(db *store.DB, now func() time.Time) *Service { return &Service{db: db, now: now} }

const activeSource = "c.state='active' AND " + store.LifecycleReadableSQL + " AND NOT " + store.PublicationTargetPendingSQL + `
 AND f.current_version_node_id=p.version_id AND g.status='active'
 AND NOT EXISTS (SELECT 1 FROM ext_document_availability n WHERE (n.tenant_id,n.knowledge_base_id,n.document_id)=(p.tenant_id,p.knowledge_base_id,p.document_id) AND n.status<>'active')
 AND EXISTS (SELECT 1 FROM ext_source_snapshot_evidence se JOIN json_each(se.snapshot_json,'$.chunks') leaf
  WHERE (se.tenant_id,se.knowledge_base_id,se.version_node_id)=(p.tenant_id,p.knowledge_base_id,p.version_id)
  AND json_extract(leaf.value,'$.id')=p.chunk_id AND json_extract(leaf.value,'$.content_hash')=p.content_hash
  AND json_extract(leaf.value,'$.chunk_index')=p.chunk_index)`

const recordsSQL = `SELECT p.feedback_id,s.revision,p.document_id,p.version_id,p.chunk_id,
 p.applies_when,p.correction,p.approved_at,p.effective_at,s.status,p.superseded_by,p.content_hash,
 CASE WHEN ` + activeSource + ` AND NOT ` + store.FamilyReplacementDueSQL + ` THEN 1 ELSE 0 END,a.effective_from,a.effective_until
 FROM ext_feedback_corrections p
 JOIN ext_qa_feedback q USING(tenant_id,knowledge_base_id,feedback_id)
 JOIN ext_feedback_staging s USING(tenant_id,knowledge_base_id,feedback_id)
 LEFT JOIN ext_feedback_guidances g ON (g.tenant_id,g.knowledge_base_id,g.guidance_id)=(p.tenant_id,p.knowledge_base_id,p.feedback_id)
 LEFT JOIN ext_document_version_family f ON (f.tenant_id,f.knowledge_base_id,f.document_id)=(p.tenant_id,p.knowledge_base_id,p.document_id)
 LEFT JOIN ext_lifecycle_current c ON (c.tenant_id,c.knowledge_base_id,c.document_id)=(p.tenant_id,p.knowledge_base_id,p.document_id)
 ` + store.LifecycleEventJoinSQL + store.LifecycleAnnotationJoinSQL + `
 WHERE p.tenant_id=? AND p.knowledge_base_id=? AND q.status='approved'`

const publishedOnly = ` AND p.superseded_by IS NULL AND s.status='active'`
const activeOnly = publishedOnly + ` AND (` + activeSource + `)`

// Historical files remain restricted to the managed directory. An archived
// entry still belongs to an existing original version and verified lifecycle.
const archivedOnly = publishedOnly + ` AND g.status='active' AND (` + store.LifecycleCurrentVerifiedSQL + `)
 AND (c.state='superseded' OR (c.state='active' AND f.current_version_node_id<>p.version_id))
 AND NOT EXISTS (SELECT 1 FROM ext_document_availability n WHERE (n.tenant_id,n.knowledge_base_id,n.document_id)=(p.tenant_id,p.knowledge_base_id,p.document_id) AND n.status<>'active')
 AND EXISTS (SELECT 1 FROM ext_document_version v WHERE (v.tenant_id,v.knowledge_base_id,v.document_id,v.version_node_id)=(p.tenant_id,p.knowledge_base_id,p.document_id,p.version_id))`

func scopeArgs(s Scope) []any { return []any{strconv.FormatUint(s.TenantID, 10), s.KnowledgeBaseID} }

func readTx(ctx context.Context, tx *sql.Tx, s Scope, now time.Time, where string, args ...any) ([]Entry, error) {
	rows, err := tx.QueryContext(ctx, recordsSQL+where, append([]any{now.UnixMilli()}, append(scopeArgs(s), args...)...)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	entries := []Entry{}
	blocked := map[string]bool{}
	for rows.Next() {
		var item Entry
		var approved, effective, stage string
		var superseded, from, until sql.NullString
		var allowed bool
		if err = rows.Scan(&item.ID, &item.Revision, &item.DocumentID, &item.VersionID, &item.ChunkID, &item.AppliesWhen, &item.Correction, &approved, &effective, &stage, &superseded, &item.contentHash, &allowed, &from, &until); err != nil {
			return nil, err
		}
		item.ApprovedAt, err = time.Parse(time.RFC3339Nano, approved)
		if err != nil {
			return nil, err
		}
		item.EffectiveAt, err = time.Parse(time.RFC3339Nano, effective)
		if err != nil {
			return nil, err
		}
		item.ApprovedAt, item.EffectiveAt = item.ApprovedAt.UTC(), item.EffectiveAt.UTC()
		item.State = "inactive"
		switch {
		case superseded.Valid:
			item.State = "superseded"
		case stage == "withdrawn":
			item.State = "withdrawn"
		case stage == "active" && allowed && !now.Before(item.EffectiveAt):
			inRange, _ := store.EffectiveAt(now, from, until)
			if inRange {
				denied, known := blocked[item.DocumentID]
				if !known {
					assessment, e := dependencies.LoadTx(ctx, tx, dependencies.Scope{TenantID: s.TenantID, KnowledgeBaseID: s.KnowledgeBaseID, DocumentID: item.DocumentID})
					if e != nil {
						return nil, e
					}
					denied = assessment.ReadBlocked(now)
					blocked[item.DocumentID] = denied
				}
				if !denied {
					item.State = "active"
				}
			}
		}
		entries = append(entries, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	slices.SortFunc(entries, func(a, b Entry) int {
		if order := b.ApprovedAt.Truncate(24 * time.Hour).Compare(a.ApprovedAt.Truncate(24 * time.Hour)); order != 0 {
			return order
		}
		return strings.Compare(a.ID, b.ID)
	})
	return entries, nil
}

// ValidID bounds incoming native identifiers without imposing UUID syntax.
func ValidID(value string) bool {
	return weknora.ValidID(value) && len(value) <= 128 && !strings.ContainsAny(value, "\x00\t ")
}

// ValidQuery bounds the native query and management search filter.
func ValidQuery(query string) bool {
	return len(query) <= 8192 && utf8.ValidString(query) && !strings.ContainsRune(query, 0)
}

// Sources finds up to twelve distinct original chunks in the authorized scope.
func (s *Service) Sources(ctx context.Context, scope Scope, ids []string, query string) ([]Source, error) {
	out := []Source{}
	if s == nil || s.db == nil {
		return nil, ErrUnavailable
	}
	if scope.TenantID == 0 || !ValidID(scope.KnowledgeBaseID) || len(ids) > 10000 || !ValidQuery(query) {
		return nil, ErrInvalid
	}
	for _, id := range ids {
		if !ValidID(id) {
			return nil, ErrInvalid
		}
	}
	terms := searchTerms(query)
	if len(ids) == 0 || len(terms) == 0 {
		return out, nil
	}
	list, _ := json.Marshal(ids)
	err := s.db.View(ctx, func(tx *sql.Tx) error {
		entries, err := readTx(ctx, tx, scope, s.now(), activeOnly+" AND p.document_id IN (SELECT value FROM json_each(?))", string(list))
		if err != nil {
			return err
		}
		seen := map[Source]bool{}
		for _, e := range entries {
			source := Source{DocumentID: e.DocumentID, ChunkID: e.ChunkID, ContentHash: e.contentHash}
			if e.State == "active" && !seen[source] && matches(terms, e) {
				out = append(out, source)
				if len(out) == 12 {
					break
				}
				seen[source] = true
			}
		}
		return nil
	})
	return out, err
}

// EvidenceTx matches only exact original chunks included in this model request.
// The caller supplies verified same-run search steps for consumed tool outputs.
func EvidenceTx(ctx context.Context, tx *sql.Tx, s Scope, now time.Time, run, step string, tools []string, query string) ([]Entry, error) {
	terms := searchTerms(query)
	if step == "" || len(terms) == 0 {
		return nil, nil
	}
	steps, _ := json.Marshal(tools)
	rows, err := readTx(ctx, tx, s, now, activeOnly+` AND EXISTS (
  SELECT 1 FROM ext_provenance_inputs i JOIN json_each(i.refs_json) ref
  WHERE (i.tenant_id,i.knowledge_base_id,i.run_id)=(p.tenant_id,p.knowledge_base_id,?)
  AND (i.step_id=? OR i.step_id IN (SELECT value FROM json_each(?))) AND i.retrieved_chunk_id=p.chunk_id
  AND json_extract(ref.value,'$.document_id')=p.document_id AND json_extract(ref.value,'$.version_id')=p.version_id
 )`, run, step, string(steps))
	if err != nil {
		return nil, err
	}
	var out []Entry
	for _, e := range rows {
		if e.State == "active" && matches(terms, e) {
			out = append(out, e)
		}
	}
	return out, nil
}

// ActiveTx validates a published entry for replacement or revision proposals.
func ActiveTx(ctx context.Context, tx *sql.Tx, s Scope, now time.Time, doc, version, id string) (Entry, error) {
	items, err := readTx(ctx, tx, s, now, activeOnly+" AND p.document_id=? AND p.version_id=? AND p.feedback_id=?", doc, version, id)
	if err != nil {
		return Entry{}, err
	}
	if len(items) != 1 || items[0].State != "active" {
		return Entry{}, ErrNotFound
	}
	return items[0], nil
}

// ConflictsTx detects different wording for one exact normalized condition.
// Different conditions require a reviewer to determine semantic overlap.
func ConflictsTx(ctx context.Context, tx *sql.Tx, scope Scope, now time.Time, entry Entry, replaces string) (bool, error) {
	items, err := readTx(ctx, tx, scope, now, activeOnly+" AND p.document_id=? AND p.version_id=? AND p.chunk_id=? AND p.feedback_id<>? AND p.feedback_id<>?", entry.DocumentID, entry.VersionID, entry.ChunkID, entry.ID, replaces)
	if err != nil {
		return false, err
	}
	for _, old := range items {
		if old.State == "active" && normalizeCondition(old.AppliesWhen) == normalizeCondition(entry.AppliesWhen) && !strings.EqualFold(strings.Join(strings.Fields(old.Correction), " "), strings.Join(strings.Fields(entry.Correction), " ")) {
			return true, nil
		}
	}
	return false, nil
}

func normalizeCondition(value string) string {
	return strings.Join(strings.FieldsFunc(strings.ToLower(value), func(r rune) bool { return unicode.IsSpace(r) || unicode.IsPunct(r) }), " ")
}

// LegacyAvailableTx preserves legacy-only guidance while applying current gates
// to any fact that has been migrated to persistent corrections.
func LegacyAvailableTx(ctx context.Context, tx *sql.Tx, s Scope, now time.Time, id string) (bool, error) {
	var doc, version string
	err := tx.QueryRowContext(ctx, "SELECT document_id,version_id FROM ext_feedback_corrections WHERE tenant_id=? AND knowledge_base_id=? AND feedback_id=?", append(scopeArgs(s), id)...).Scan(&doc, &version)
	if errors.Is(err, sql.ErrNoRows) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	_, err = ActiveTx(ctx, tx, s, now, doc, version, id)
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	return err == nil, err
}

func matches(terms map[string]bool, e Entry) bool {
	for term := range searchTerms(e.AppliesWhen + " " + e.Correction) {
		if terms[term] {
			return true
		}
	}
	return false
}

// Prefer shared specific words, CJK bigrams, or number-plus-unit phrases.
// Single digits, punctuation and generic question words are insufficient.
func searchTerms(text string) map[string]bool {
	terms := map[string]bool{}
	var word, number, unit strings.Builder
	var lastHan rune
	for _, r := range strings.ToLower(text) + " " {
		if unicode.Is(unicode.Han, r) {
			if lastHan != 0 {
				pair := string([]rune{lastHan, r})
				if !strings.Contains("|哪项|规定|什么|多少|如何|是否|请问|可以|这个|哪个|有关|根据|适用|条件|问题|关于|内容|", "|"+pair+"|") {
					terms[pair] = true
				}
			}
			lastHan = r
			if number.Len() != 0 {
				unit.WriteRune(r)
			}
		} else {
			lastHan = 0
			if unit.Len() != 0 {
				suffix := strings.TrimPrefix(unit.String(), "个")
				for _, u := range []string{"工作日", "自然日", "日", "天", "月", "年", "小时", "分钟"} {
					if strings.HasPrefix(suffix, u) {
						terms[number.String()+u] = true
						break
					}
				}
				number.Reset()
				unit.Reset()
			}
			if unicode.IsDigit(r) {
				number.WriteRune(r)
			} else if !unicode.IsSpace(r) {
				number.Reset()
			}
		}
		if !unicode.Is(unicode.Han, r) && unicode.IsLetter(r) {
			word.WriteRune(r)
		} else if word.Len() != 0 {
			w := word.String()
			if len(w) >= 3 && !strings.Contains("|the|this|that|these|those|from|about|how|many|what|where|when|why|which|who|does|did|was|has|are|and|with|answer|answering|clause|regulation|policy|conditions|correction|document|rule|rules|please|should|would|could|can|will|", "|"+w+"|") {
				if len(w) > 4 && !strings.HasSuffix(w, "ss") {
					w = strings.TrimSuffix(w, "s")
				}
				terms[w] = true
			}
			word.Reset()
		}
	}
	return terms
}
