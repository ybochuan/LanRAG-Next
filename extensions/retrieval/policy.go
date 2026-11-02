package retrieval

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/contract"
	"github.com/ybochuan/LanRAG-Next/extensions/dependencies"
	"github.com/ybochuan/LanRAG-Next/extensions/lineage"
	"github.com/ybochuan/LanRAG-Next/extensions/provenance"
	"github.com/ybochuan/LanRAG-Next/extensions/store"
)

// Policy permits only known active lifecycle facts in its configured scopes.
// Missing facts are excluded until reconciliation confirms the source document.
type Policy struct {
	db      *store.DB
	tenant  uint64
	kbs     map[string]bool
	sources SourceRefresher
}

// SourceRefresher reacquires scoped native evidence; no cached permit is accepted.
type SourceRefresher interface {
	Refresh(context.Context, provenance.Scope, string) (lineage.Version, error)
}

// WithSources installs the production registry before the policy is served.
func (p *Policy) WithSources(s SourceRefresher) *Policy { p.sources = s; return p }

func dependencyTargets(ctx context.Context, tx *sql.Tx, r contract.BeforeRetrieveRequest) ([]dependencies.Scope, error) {
	placeholders := func(n int) string { return strings.TrimSuffix(strings.Repeat("?,", n), ",") }
	where := "tenant_id=? AND knowledge_base_id IN (" + placeholders(len(r.KnowledgeBaseIDs)) + ") AND document_id IN (" + placeholders(len(r.KnowledgeIDs)) + ")"
	args := []any{fmt.Sprint(r.TenantID)}
	for _, kb := range r.KnowledgeBaseIDs {
		args = append(args, kb)
	}
	for _, doc := range r.KnowledgeIDs {
		args = append(args, doc)
	}
	both := append(append([]any{}, args...), args...)
	rows, e := tx.QueryContext(ctx, "SELECT knowledge_base_id,document_id FROM ext_supplement_declarations WHERE "+where+" UNION SELECT knowledge_base_id,document_id FROM ext_supplement_current WHERE "+where+" ORDER BY knowledge_base_id,document_id", both...)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var targets []dependencies.Scope
	for rows.Next() {
		scope := dependencies.Scope{TenantID: r.TenantID}
		if e = rows.Scan(&scope.KnowledgeBaseID, &scope.DocumentID); e != nil {
			return nil, e
		}
		targets = append(targets, scope)
	}
	return targets, rows.Err()
}

// NewPolicy binds a single tenant and explicit KB allowlist; DB errors never permit.
func NewPolicy(db *store.DB, tenant uint64, kbs []string) (*Policy, error) {
	if db == nil || (contract.Context{TenantID: tenant, KnowledgeBaseIDs: kbs}).ValidateScope() != nil {
		return nil, errors.New("database and explicit policy scopes required")
	}
	p := &Policy{db: db, tenant: tenant, kbs: make(map[string]bool, len(kbs))}
	for _, kb := range kbs {
		p.kbs[kb] = true
	}
	return p, nil
}

// ExcludedDocumentIDs returns a subset of the supplied native-authorized IDs.
// If the same ID ambiguously appears in multiple requested KBs, any non-active
// record wins. No document metadata or out-of-scope IDs enter the response.
func (p *Policy) ExcludedDocumentIDs(ctx context.Context, r contract.BeforeRetrieveRequest) ([]string, error) {
	if r.Validate() != nil || r.TenantID != p.tenant {
		return nil, errors.New("invalid retrieval policy scope")
	}
	for _, kb := range r.KnowledgeBaseIDs {
		if !p.kbs[kb] {
			return nil, errors.New("retrieval KB outside policy scope")
		}
	}
	if len(r.KnowledgeIDs) == 0 {
		return []string{}, ctx.Err()
	}
	// Collect only dependencies of native-authorized targets, refresh outside the
	// final read transaction, then compare every pin again in that transaction.
	verified := map[dependencies.Scope]string{}
	var refs []dependencies.Scope
	err := p.db.View(ctx, func(tx *sql.Tx) error {
		targets, e := dependencyTargets(ctx, tx, r)
		if e != nil {
			return e
		}
		seen := map[dependencies.Scope]bool{}
		for _, scope := range targets {
			a, e := dependencies.LoadTx(ctx, tx, scope)
			if e != nil {
				return e
			}
			for _, ref := range a.Sources {
				s := dependencies.Scope{TenantID: scope.TenantID, KnowledgeBaseID: scope.KnowledgeBaseID, DocumentID: ref.DocumentID}
				if !seen[s] {
					seen[s] = true
					refs = append(refs, s)
				}
			}
		}
		if len(refs) > 256 {
			return errors.New("dependency verification limit")
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(refs) > 0 && p.sources == nil {
		return nil, errors.New("dependency source authority unavailable")
	}
	for _, scope := range refs {
		v, e := p.sources.Refresh(ctx, provenance.Scope{TenantID: scope.TenantID, KnowledgeBaseID: scope.KnowledgeBaseID}, scope.DocumentID)
		if e != nil || v.DocumentID != scope.DocumentID || v.ID == "" {
			return nil, errors.New("dependency source verification failed")
		}
		verified[scope] = v.ID
	}
	active := make(map[string]bool, len(r.KnowledgeIDs))
	blocked := make(map[string]bool)
	placeholders := func(n int) string { return strings.TrimSuffix(strings.Repeat("?,", n), ",") }
	args := []any{fmt.Sprint(r.TenantID)}
	for _, kb := range r.KnowledgeBaseIDs {
		args = append(args, kb)
	}
	for _, id := range r.KnowledgeIDs {
		args = append(args, id)
	}
	err = p.db.View(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT c.document_id,CASE WHEN `+store.LifecycleCurrentVerifiedSQL+` THEN c.state ELSE '' END FROM ext_lifecycle_current c `+store.LifecycleEventJoinSQL+` WHERE c.tenant_id=? AND c.knowledge_base_id IN (`+placeholders(len(r.KnowledgeBaseIDs))+`) AND c.document_id IN (`+placeholders(len(r.KnowledgeIDs))+`)`, args...)
		if err != nil {
			return err
		}
		for rows.Next() {
			var id, state string
			if err = rows.Scan(&id, &state); err != nil {
				rows.Close()
				return err
			}
			if state == "active" {
				active[id] = true
			} else {
				blocked[id] = true
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		targets, e := dependencyTargets(ctx, tx, r)
		if e != nil {
			return e
		}
		for _, scope := range targets {
			a, e := dependencies.LoadTx(ctx, tx, scope)
			if e != nil {
				return e
			}
			if a.Invalid(time.Now().UTC()) {
				blocked[scope.DocumentID] = true
			}
			for _, ref := range a.Sources {
				s := dependencies.Scope{TenantID: scope.TenantID, KnowledgeBaseID: scope.KnowledgeBaseID, DocumentID: ref.DocumentID}
				if verified[s] != ref.VersionID {
					blocked[scope.DocumentID] = true
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	excluded := make([]string, 0)
	for _, id := range r.KnowledgeIDs {
		if !active[id] || blocked[id] {
			excluded = append(excluded, id)
		}
	}
	return excluded, nil
}
