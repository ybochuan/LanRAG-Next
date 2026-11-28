package taggov

import (
	"context"
	"errors"
	"strings"

	"github.com/ybochuan/LanRAG-Next/extensions/lifecycle"
	"github.com/ybochuan/LanRAG-Next/extensions/weknora"
)

// Preconditioner checks whether a native KB already exposes the canonical
// lifecycle tags that governed projections need. It never creates a tag.
type Preconditioner struct{ catalog Catalog }

// NewPreconditioner shares the native catalog reader.
func NewPreconditioner(catalog Catalog) *Preconditioner { return &Preconditioner{catalog: catalog} }

// LifecycleStateTagNames returns the canonical state tags in stable order.
func LifecycleStateTagNames() []string {
	return []string{
		"gov:state:" + string(lifecycle.Pending),
		"gov:state:" + string(lifecycle.Scheduled),
		"gov:state:" + string(lifecycle.Active),
		"gov:state:" + string(lifecycle.Stale),
	}
}

// MissingLifecycleStateTags reads the full KB-local catalog once and returns
// canonical state tags that are absent. A read failure is returned as-is; the
// caller must not report a clean precondition on an unreadable catalog.
func (p *Preconditioner) MissingLifecycleStateTags(ctx context.Context, tenantID uint64, kbID string) ([]string, error) {
	if p == nil || p.catalog == nil || tenantID == 0 || !weknora.ValidID(kbID) {
		return nil, errors.New("invalid tag preconditioner configuration or scope")
	}
	// Catalog uses only tenant and KB; the document slot is filled with the KB
	// ID solely to satisfy the shared scope shape.
	tags, err := p.catalog.Catalog(ctx, Scope{TenantID: tenantID, KnowledgeBaseID: kbID, DocumentID: kbID})
	if err != nil {
		return nil, err
	}
	present := make(map[string]bool, len(tags))
	for _, tag := range tags {
		present[strings.TrimSpace(tag.Name)] = true
	}
	missing := make([]string, 0, 4)
	for _, name := range LifecycleStateTagNames() {
		if !present[name] {
			missing = append(missing, name)
		}
	}
	return missing, nil
}
