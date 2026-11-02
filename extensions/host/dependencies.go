package host

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/dependencies"
	"github.com/ybochuan/LanRAG-Next/extensions/lifecycle"
	"github.com/ybochuan/LanRAG-Next/extensions/lineage"
	"github.com/ybochuan/LanRAG-Next/extensions/provenance"
	"github.com/ybochuan/LanRAG-Next/extensions/store"
)

// refreshDependencies uses bounded pages and never manufactures an invalid
// lifecycle fact from a network error. Permission independently refreshes roots.
func refreshDependencies(ctx context.Context, db *store.DB, registry *lineage.Registry, scopes []lifecycle.PolicyScope) error {
	var failures []error
	for _, scope := range scopes {
		cursor := ""
		for {
			var ids []string
			e := db.View(ctx, func(tx *sql.Tx) error {
				rows, e := tx.QueryContext(ctx, `SELECT DISTINCT document_id FROM ext_supplement_declarations WHERE tenant_id=? AND knowledge_base_id=? AND document_id>? ORDER BY document_id LIMIT 128`, strconv.FormatUint(scope.TenantID, 10), scope.KnowledgeBaseID, cursor)
				if e != nil {
					return e
				}
				defer rows.Close()
				for rows.Next() {
					var id string
					if e = rows.Scan(&id); e != nil {
						return e
					}
					ids = append(ids, id)
				}
				return rows.Err()
			})
			if e != nil {
				return e
			}
			for _, id := range ids {
				var a dependencies.Assessment
				e = db.View(ctx, func(tx *sql.Tx) error {
					var e error
					a, e = dependencies.LoadTx(ctx, tx, dependencies.Scope{TenantID: scope.TenantID, KnowledgeBaseID: scope.KnowledgeBaseID, DocumentID: id})
					return e
				})
				if e == nil {
					for _, ref := range a.Sources {
						if _, x := registry.Refresh(ctx, provenance.Scope{TenantID: scope.TenantID, KnowledgeBaseID: scope.KnowledgeBaseID}, ref.DocumentID); x != nil {
							e = x
							break
						}
					}
				}
				if ctx.Err() != nil {
					return ctx.Err()
				}
				if e != nil && len(failures) < 16 {
					failures = append(failures, errors.New("dependency refresh failed"))
				}
			}
			if len(ids) < 128 {
				break
			}
			cursor = ids[len(ids)-1]
		}
	}
	return errors.Join(failures...)
}

// runDependencyDaily joins the existing Host task lifetime; local readiness does
// not depend on Core, and local dates still advance when Core cannot be reached.
func runDependencyDaily(ctx context.Context, db *store.DB, registry *lineage.Registry, machine *lifecycle.Machine, scopes []lifecycle.PolicyScope, report func(string)) error {
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()
	for {
		if e := refreshDependencies(ctx, db, registry, scopes); e != nil && ctx.Err() == nil {
			report("dependency_refresh_failed")
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if e := machine.Sweep(ctx, scopes); e != nil && ctx.Err() == nil {
			report("lifecycle_scan_failed")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
