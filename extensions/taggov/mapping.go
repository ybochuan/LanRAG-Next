package taggov

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/store"
)

// Catalog reads a complete native KB-local catalog, not a cache of old IDs.
type Catalog interface {
	Catalog(context.Context, Scope) ([]Tag, error)
}

// Mapping persists verified canonical-name to native-ID bindings per tenant/KB.
type Mapping struct {
	db      *store.DB
	catalog Catalog
}

// NewMapping shares its dependencies and never creates tags upstream.
func NewMapping(db *store.DB, catalog Catalog) *Mapping { return &Mapping{db, catalog} }

// Resolve refreshes from the native catalog before every write. Canonical tags
// must already exist in that KB; an absent/ambiguous mapping is never guessed.
func (m *Mapping) Resolve(ctx context.Context, scope Scope, name string) (Tag, error) {
	if m.db == nil || m.catalog == nil || !validScope(scope) {
		return Tag{}, errors.New("invalid mapping configuration or scope")
	}
	governed := false
	for _, prefix := range []string{"gov:state:", "gov:domain:", "gov:family:", "gov:relation:", "gov:parse:"} {
		if strings.HasPrefix(name, prefix) && len(name) > len(prefix) {
			governed = true
		}
	}
	if !governed {
		return Tag{}, errors.New("canonical governance tag name required")
	}
	tags, err := m.catalog.Catalog(ctx, scope)
	if err != nil {
		return Tag{}, err
	}
	var selected Tag
	for _, tag := range tags {
		if tag.Name == name {
			if selected.ID != "" {
				return Tag{}, errors.New("ambiguous canonical tag")
			}
			selected = tag
		}
	}
	if selected.ID == "" {
		return Tag{}, errors.New("canonical tag not configured in native KB")
	}
	err = m.db.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO ext_kb_tag_mapping(tenant_id,knowledge_base_id,canonical_name,tag_id,verified_at) VALUES(?,?,?,?,?)
		ON CONFLICT(tenant_id,knowledge_base_id,canonical_name) DO UPDATE SET tag_id=excluded.tag_id,verified_at=excluded.verified_at`, strconv.FormatUint(scope.TenantID, 10), scope.KnowledgeBaseID, name, selected.ID, time.Now().UTC().UnixMilli())
		return err
	})
	if err != nil {
		return Tag{}, err
	}
	return selected, nil
}
