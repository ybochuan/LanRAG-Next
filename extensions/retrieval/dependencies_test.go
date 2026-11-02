package retrieval

import (
	"context"
	"database/sql"
	"errors"
	"github.com/ybochuan/LanRAG-Next/extensions/lineage"
	"github.com/ybochuan/LanRAG-Next/extensions/provenance"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/contract"
	"github.com/ybochuan/LanRAG-Next/extensions/lifecycle"
	"github.com/ybochuan/LanRAG-Next/extensions/store"
)

func dependencyFixture(t *testing.T) (*store.DB, contract.BeforeRetrieveRequest) {
	t.Helper()
	ctx := context.Background()
	db, e := store.OpenDurable(ctx, filepath.Join(t.TempDir(), "deps.db"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { db.Close() })
	for _, doc := range []string{"child", "parent"} {
		if _, e = lifecycle.NewStore(db).Append(ctx, lifecycle.Event{EventID: doc, TenantID: 7, KnowledgeBaseID: "kb", DocumentID: doc, ToState: lifecycle.Active, Reason: "fixture", OccurredAt: time.Now()}); e != nil {
			t.Fatal(e)
		}
		e = db.Write(ctx, func(tx *sql.Tx) error {
			if _, e := tx.Exec("INSERT INTO ext_lineage_node VALUES(?,'7','kb','document_version',?,'2026-11-01T00:00:00Z')", doc+"-v", doc); e != nil {
				return e
			}
			if _, e := tx.Exec("INSERT INTO ext_document_version_family VALUES(?,'7','kb',?,'',?,?,1,'2026-11-01T00:00:00Z','2026-11-01T00:00:00Z')", doc, doc, doc+"-v", doc+"-v"); e != nil {
				return e
			}
			_, e := tx.Exec("INSERT INTO ext_document_version VALUES(?,'7','kb',?,?,?,1,?,'','fixture','2026-11-01T00:00:00Z')", doc+"-v", doc, doc, strings.Repeat("a", 64), doc)
			return e
		})
		if e != nil {
			t.Fatal(e)
		}
	}
	e = db.Write(ctx, func(tx *sql.Tx) error {
		if _, e := tx.Exec("INSERT INTO ext_supplement_declarations VALUES('decl','7','kb','child',1,'child-v','child','actor','op',?,'active','2026-11-01T00:00:00Z')", strings.Repeat("b", 64)); e != nil {
			return e
		}
		_, e := tx.Exec("INSERT INTO ext_supplement_parents VALUES('decl','7','kb','child','parent','parent-v','parent');INSERT INTO ext_supplement_current VALUES('7','kb','child',1,'decl')")
		return e
	})
	if e != nil {
		t.Fatal(e)
	}
	req := contract.BeforeRetrieveRequest{Context: contract.Context{RequestID: "r", SessionID: "s", Mode: "rag", TenantID: 7, KnowledgeBaseIDs: []string{"kb"}, KnowledgeIDs: []string{"child"}}}
	return db, req
}
func TestDeclaredDependencyWithoutFreshAuthorityCannotPermit(t *testing.T) {
	db, req := dependencyFixture(t)
	p, e := NewPolicy(db, 7, []string{"kb"})
	if e != nil {
		t.Fatal(e)
	}
	ctx := context.Background()
	got, e := p.ExcludedDocumentIDs(ctx, req)
	if e == nil && len(got) != 1 {
		t.Fatal("dependency admitted without current native proof", got)
	}
}

type refreshFunc func(context.Context, provenance.Scope, string) (lineage.Version, error)

func (f refreshFunc) Refresh(ctx context.Context, s provenance.Scope, id string) (lineage.Version, error) {
	return f(ctx, s, id)
}
func TestDependencyPolicyRechecksLatestFactsAfterSourceRefresh(t *testing.T) {
	for _, mode := range []string{"active", "offline", "state_race", "expired", "version_race"} {
		t.Run(mode, func(t *testing.T) {
			db, req := dependencyFixture(t)
			ctx := context.Background()
			p, e := NewPolicy(db, 7, []string{"kb"})
			if e != nil {
				t.Fatal(e)
			}
			p.WithSources(refreshFunc(func(_ context.Context, s provenance.Scope, doc string) (lineage.Version, error) {
				if s.TenantID != 7 || s.KnowledgeBaseID != "kb" {
					t.Fatal("scope expanded")
				}
				if mode == "offline" {
					return lineage.Version{}, errors.New("source offline")
				}
				if doc == "parent" {
					switch mode {
					case "state_race":
						_, e := lifecycle.NewStore(db).Append(ctx, lifecycle.Event{EventID: "stale", TenantID: 7, KnowledgeBaseID: "kb", DocumentID: doc, FromState: lifecycle.Active, ToState: lifecycle.Stale, Reason: "fixture", OccurredAt: time.Now()})
						if e != nil {
							t.Fatal(e)
						}
					case "expired":
						past := time.Now().Add(-time.Hour)
						if e := lifecycle.NewConfiguration(db).PutAnnotations(ctx, lifecycle.Scope{TenantID: 7, KnowledgeBaseID: "kb", DocumentID: doc}, lifecycle.Annotations{EffectiveUntil: &past}, 0); e != nil {
							t.Fatal(e)
						}
					case "version_race":
						if e := db.Write(ctx, func(tx *sql.Tx) error {
							if _, e := tx.Exec("INSERT INTO ext_lineage_node VALUES('parent-v2','7','kb','document_version','parent','2026-11-01T00:00:00Z')"); e != nil {
								return e
							}
							_, e := tx.Exec("UPDATE ext_document_version_family SET current_version_node_id='parent-v2' WHERE document_id='parent'")
							return e
						}); e != nil {
							t.Fatal(e)
						}
					}
				}
				return lineage.Version{ID: doc + "-v", DocumentID: doc}, nil
			}))
			got, e := p.ExcludedDocumentIDs(ctx, req)
			if mode == "active" {
				if e != nil || len(got) != 0 {
					t.Fatal("valid sources denied", got, e)
				}
			} else if e == nil && (len(got) != 1 || got[0] != "child") {
				t.Fatal("invalid dependency permitted", got, e)
			}
		})
	}
}
