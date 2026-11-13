// Package dependencies reads declared governance dependencies across
// knowledge bases. It owns no scheduler and never modifies lifecycle,
// annotations or original documents.
package dependencies

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"strconv"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/store"
)

// Scope identifies a native-authorized child document.
type Scope struct {
	TenantID        uint64 `json:"tenant_id"`
	KnowledgeBaseID string `json:"knowledge_base_id"`
	DocumentID      string `json:"document_id"`
}

// SourceRef pins an actual observed version; callers refresh these roots before permission.
type SourceRef struct {
	KnowledgeBaseID string `json:"knowledge_base_id"`
	DocumentID      string `json:"document_id"`
	VersionID       string `json:"version_id"`
}

// Parent describes a declared, scoped ancestor's current governance evidence.
type Parent struct {
	KnowledgeBaseID string     `json:"knowledge_base_id"`
	DocumentID      string     `json:"document_id"`
	VersionID       string     `json:"version_id"`
	State           string     `json:"state"`
	EffectiveFrom   *time.Time `json:"effective_from,omitempty"`
	EffectiveUntil  *time.Time `json:"effective_until,omitempty"`
	ReviewDue       *time.Time `json:"review_due,omitempty"`
}

// Assessment is a snapshot of the complete declared dependency closure.
type Assessment struct {
	Configured       bool        `json:"configured"`
	Revision         int64       `json:"revision"`
	StructuralReason string      `json:"reason,omitempty"`
	Parents          []Parent    `json:"parents"`
	Sources          []SourceRef `json:"sources"`
	ReviewDue        *time.Time  `json:"review_due,omitempty"`
	RepealHold       bool        `json:"repeal_hold,omitempty"`
	PublicationHold  bool        `json:"publication_hold,omitempty"`
}

// ReadBlocked includes temporary holds that must not persist a lifecycle transition.
func (a Assessment) ReadBlocked(now time.Time) bool {
	return a.RepealHold || a.PublicationHold || a.Invalid(now)
}

// Invalid distinguishes permission invalidation from advisory review deadlines.
func (a Assessment) Invalid(now time.Time) bool {
	if a.StructuralReason != "" {
		return true
	}
	for _, p := range a.Parents {
		if p.State != "active" || p.EffectiveFrom != nil && now.Before(*p.EffectiveFrom) || p.EffectiveUntil != nil && now.After(*p.EffectiveUntil) {
			return true
		}
	}
	return false
}

// ReviewOverdue is advisory and never a lifecycle transition.
func (a Assessment) ReviewOverdue(now time.Time) bool {
	return a.ReviewDue != nil && now.After(*a.ReviewDue)
}
func scoped(s Scope, tail ...any) []any {
	return append([]any{strconv.FormatUint(s.TenantID, 10), s.KnowledgeBaseID}, tail...)
}

func dependencyKey(kb, doc string) string { return kb + "\x00" + doc }

// LoadTx reads current declarations, versions, lifecycle and dates in one view.
// A malformed/cyclic graph returns an error, never an empty/allowed assessment.
func LoadTx(ctx context.Context, tx *sql.Tx, s Scope) (Assessment, error) {
	a := Assessment{Parents: []Parent{}, Sources: []SourceRef{}}
	if s.TenantID == 0 || s.KnowledgeBaseID == "" || s.DocumentID == "" {
		return a, errors.New("explicit dependency scope required")
	}
	path := map[string]bool{}
	seen := map[string]bool{}
	height := map[string]int{}
	sources := map[string]SourceRef{}
	addSource := func(kb, doc, version string) {
		key := dependencyKey(kb, doc)
		if old, ok := sources[key]; ok && old.VersionID != version {
			a.StructuralReason = "source_version_changed"
		}
		sources[key] = SourceRef{KnowledgeBaseID: kb, DocumentID: doc, VersionID: version}
	}
	var visit func(Scope, int) error
	visit = func(sc Scope, depth int) error {
		key := dependencyKey(sc.KnowledgeBaseID, sc.DocumentID)
		if path[key] || depth > 10 {
			return errors.New("dependency cycle or traversal limit")
		}
		if seen[key] {
			if depth+height[key] > 10 {
				return errors.New("dependency traversal limit")
			}
			return nil
		}
		if len(seen) >= 64 {
			return errors.New("dependency traversal limit")
		}
		seen[key] = true
		path[key] = true
		defer delete(path, key)
		var id, status, childVersion string
		var revision int64
		e := tx.QueryRowContext(ctx, `SELECT d.declaration_id,d.status,d.child_version_node_id,d.revision FROM ext_supplement_current c JOIN ext_supplement_declarations d ON d.tenant_id=c.tenant_id AND d.knowledge_base_id=c.knowledge_base_id AND d.document_id=c.document_id AND d.declaration_id=c.declaration_id AND d.revision=c.revision WHERE c.tenant_id=? AND c.knowledge_base_id=? AND c.document_id=?`, scoped(sc, sc.DocumentID)...).Scan(&id, &status, &childVersion, &revision)
		if errors.Is(e, sql.ErrNoRows) {
			var exists bool
			if e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM ext_supplement_declarations WHERE tenant_id=? AND knowledge_base_id=? AND document_id=?) OR EXISTS(SELECT 1 FROM ext_supplement_current WHERE tenant_id=? AND knowledge_base_id=? AND document_id=?)`, append(scoped(sc, sc.DocumentID), scoped(sc, sc.DocumentID)...)...).Scan(&exists); e != nil {
				return e
			}
			if exists {
				return errors.New("inconsistent dependency current declaration")
			}
			return nil
		}
		if e != nil {
			return e
		}
		var latest int64
		if e = tx.QueryRowContext(ctx, "SELECT MAX(revision) FROM ext_supplement_declarations WHERE tenant_id=? AND knowledge_base_id=? AND document_id=?", scoped(sc, sc.DocumentID)...).Scan(&latest); e != nil {
			return e
		}
		if latest != revision {
			return errors.New("dependency head is not latest declaration")
		}
		if sc.KnowledgeBaseID == s.KnowledgeBaseID && sc.DocumentID == s.DocumentID {
			a.Revision = revision
		}
		if status == "revoked" {
			return nil
		}
		if status != "active" {
			return errors.New("invalid dependency declaration")
		}
		a.Configured = true
		addSource(sc.KnowledgeBaseID, sc.DocumentID, childVersion)
		var current string
		e = tx.QueryRowContext(ctx, "SELECT current_version_node_id FROM ext_document_version_family WHERE tenant_id=? AND knowledge_base_id=? AND document_id=?", scoped(sc, sc.DocumentID)...).Scan(&current)
		if e != nil && !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		if e != nil || current != childVersion {
			a.StructuralReason = "source_version_changed"
		}
		rows, e := tx.QueryContext(ctx, `SELECT knowledge_base_id,parent_document_id,parent_version_node_id
			FROM ext_supplement_parents
			WHERE tenant_id=? AND knowledge_base_id=? AND document_id=? AND declaration_id=?
			UNION ALL
			SELECT parent_knowledge_base_id,parent_document_id,parent_version_node_id
			FROM ext_supplement_cross_parents
			WHERE tenant_id=? AND knowledge_base_id=? AND document_id=? AND declaration_id=?
			ORDER BY 1,2 LIMIT 9`, append(scoped(sc, sc.DocumentID, id), scoped(sc, sc.DocumentID, id)...)...)
		if e != nil {
			return e
		}
		refs := []SourceRef{}
		for rows.Next() {
			var ref SourceRef
			if e = rows.Scan(&ref.KnowledgeBaseID, &ref.DocumentID, &ref.VersionID); e != nil {
				rows.Close()
				return e
			}
			refs = append(refs, ref)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return e
		}
		if len(refs) == 0 || len(refs) > 8 {
			return errors.New("incomplete dependency declaration")
		}
		for _, ref := range refs {
			addSource(ref.KnowledgeBaseID, ref.DocumentID, ref.VersionID)
			parentScope := Scope{TenantID: s.TenantID, KnowledgeBaseID: ref.KnowledgeBaseID, DocumentID: ref.DocumentID}
			state, _, e := store.ReadLifecycleCurrentTx(ctx, tx, parentScope.TenantID, parentScope.KnowledgeBaseID, parentScope.DocumentID)
			if e != nil && !errors.Is(e, sql.ErrNoRows) {
				return e
			}
			if e != nil {
				a.StructuralReason = "parent_missing"
			}
			if state == "active" {
				var repealDue, publicationPending bool
				e = tx.QueryRowContext(ctx, `SELECT `+store.RepealDueSQL+`,`+store.PublicationTargetPendingSQL+` FROM ext_lifecycle_current c
					WHERE c.tenant_id=? AND c.knowledge_base_id=? AND c.document_id=?`, scoped(parentScope, ref.DocumentID)...).Scan(&repealDue, &publicationPending)
				if e != nil {
					return e
				}
				a.RepealHold = a.RepealHold || repealDue
				a.PublicationHold = a.PublicationHold || publicationPending
			}
			p := Parent{KnowledgeBaseID: ref.KnowledgeBaseID, DocumentID: ref.DocumentID, VersionID: ref.VersionID, State: state}
			current = ""
			e = tx.QueryRowContext(ctx, "SELECT current_version_node_id FROM ext_document_version_family WHERE tenant_id=? AND knowledge_base_id=? AND document_id=?", scoped(parentScope, ref.DocumentID)...).Scan(&current)
			if e != nil && !errors.Is(e, sql.ErrNoRows) {
				return e
			}
			if e != nil || current != ref.VersionID {
				a.StructuralReason = "source_version_changed"
			}
			var from, until, review sql.NullString
			e = tx.QueryRowContext(ctx, "SELECT effective_from,effective_until,review_due FROM ext_document_annotations WHERE tenant_id=? AND knowledge_base_id=? AND document_id=?", scoped(parentScope, ref.DocumentID)...).Scan(&from, &until, &review)
			if e != nil && !errors.Is(e, sql.ErrNoRows) {
				return e
			}
			for _, field := range []struct {
				raw  sql.NullString
				dest **time.Time
			}{{from, &p.EffectiveFrom}, {until, &p.EffectiveUntil}, {review, &p.ReviewDue}} {
				if field.raw.Valid {
					date, x := time.Parse(time.RFC3339Nano, field.raw.String)
					if x != nil {
						return errors.New("invalid dependency date")
					}
					*field.dest = &date
				}
			}
			if p.ReviewDue != nil && (a.ReviewDue == nil || p.ReviewDue.Before(*a.ReviewDue)) {
				a.ReviewDue = p.ReviewDue
			}
			a.Parents = append(a.Parents, p)
			if e = visit(parentScope, depth+1); e != nil {
				return e
			}
			parentKey := dependencyKey(parentScope.KnowledgeBaseID, parentScope.DocumentID)
			if height[parentKey]+1 > height[key] {
				height[key] = height[parentKey] + 1
			}
		}
		return nil
	}
	if e := visit(s, 0); e != nil {
		return Assessment{}, e
	}
	for _, source := range sources {
		a.Sources = append(a.Sources, source)
	}
	sort.Slice(a.Sources, func(i, j int) bool {
		if a.Sources[i].KnowledgeBaseID != a.Sources[j].KnowledgeBaseID {
			return a.Sources[i].KnowledgeBaseID < a.Sources[j].KnowledgeBaseID
		}
		return a.Sources[i].DocumentID < a.Sources[j].DocumentID
	})
	return a, nil
}
