package management

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ybochuan/LanRAG-Next/extensions/lifecycle"
	"github.com/ybochuan/LanRAG-Next/extensions/store"
)

type document struct {
	DocumentID     string                `json:"document_id"`
	Title          string                `json:"title,omitempty"`
	NativeTitle    string                `json:"native_title,omitempty"`
	DisplayName    string                `json:"display_name,omitempty"`
	MappingStatus  string                `json:"mapping_status,omitempty"`
	AnnexOriginal  string                `json:"annex_original,omitempty"`
	State          string                `json:"state"`
	LastEventID    string                `json:"last_event_id"`
	Annotations    lifecycle.Annotations `json:"annotations"`
	BusinessFamily *documentFamily       `json:"business_family,omitempty"`
	Degraded       bool                  `json:"degraded,omitempty"`
	DegradedReason string                `json:"degraded_reason,omitempty"`
}

type documentFamily struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// enrichDocumentFamilies uses confirmed membership, independently of legacy projections.
// enrichAttachmentTitles overlays a recognised attachment name on documents
// whose native title is a generic annex filename. The native title survives in
// NativeTitle; confirmed/candidate mappings are surfaced for the UI.
func (s *Server) enrichAttachmentTitles(ctx context.Context, r Request, documents []document) {
	if len(documents) == 0 {
		return
	}
	ids := make([]string, 0, len(documents))
	for _, d := range documents {
		ids = append(ids, d.DocumentID)
	}
	semantics, err := s.db.AttachmentSemantics(ctx, r.TenantID, ids)
	if err != nil || len(semantics) == 0 {
		return
	}
	for i := range documents {
		d := &documents[i]
		sem, ok := semantics[d.DocumentID]
		if !ok || sem.DisplayName == "" {
			continue
		}
		if !isGenericAnnexTitle(d.Title) || strings.EqualFold(sem.DisplayName, d.Title) {
			if d.Title != "" {
				d.NativeTitle = d.Title
			}
			continue
		}
		d.NativeTitle = d.Title
		d.Title = sem.DisplayName
		d.DisplayName = sem.DisplayName
		d.MappingStatus = sem.MappingStatus
		d.AnnexOriginal = sem.OriginalFilename
	}
}

func isGenericAnnexTitle(title string) bool {
	t := strings.ToLower(strings.TrimSpace(title))
	if !strings.HasPrefix(t, "annex-") {
		return false
	}
	ext := filepath.Ext(t)
	return ext != "" && len(t) > len("annex-")+len(ext)
}

func (s *Server) enrichDocumentFamilies(ctx context.Context, r Request, documents []document) error {
	if len(documents) == 0 {
		return nil
	}
	ids := make([]string, len(documents))
	for i := range documents {
		ids[i] = documents[i].DocumentID
	}
	body, _ := json.Marshal(ids)
	return s.db.View(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT m.document_id,m.business_family_id,f.label
FROM ext_business_document_family_members m
JOIN ext_business_document_families f USING(tenant_id,knowledge_base_id,business_family_id)
WHERE m.tenant_id=? AND m.knowledge_base_id=? AND m.document_id IN (SELECT value FROM json_each(?))`,
			strconv.FormatUint(r.TenantID, 10), r.KnowledgeBaseID, string(body))
		if err != nil {
			return err
		}
		defer rows.Close()
		families := make(map[string]*documentFamily)
		for rows.Next() {
			var id string
			family := new(documentFamily)
			if err := rows.Scan(&id, &family.ID, &family.Label); err != nil {
				return err
			}
			families[id] = family
		}
		for i := range documents {
			documents[i].BusinessFamily = families[documents[i].DocumentID]
		}
		return rows.Err()
	})
}

func (s *Server) rules(ctx context.Context, r Request) (map[string]any, error) {
	resolved, local, err := lifecycle.NewConfiguration(s.db).ResolveForEdit(ctx, lifecycle.PolicyScope{TenantID: r.TenantID, KnowledgeBaseID: r.KnowledgeBaseID})
	if err != nil {
		return nil, errDatabase
	}
	return map[string]any{"rules": resolved, "rule_revision": local}, nil
}
func (s *Server) overview(ctx context.Context, r Request) (any, error) {
	if r.Page == 0 {
		r.Page = 1
	}
	if r.PageSize == 0 {
		r.PageSize = 20
	}
	if r.Page < 1 || r.Page > 100000 || r.PageSize < 1 || r.PageSize > 100 {
		return nil, errors.New("invalid page")
	}
	counts := map[string]int{"pending": 0, "scheduled": 0, "active": 0, "stale": 0, "archived": 0, "superseded": 0, "failed": 0}
	if _, ok := counts[r.State]; r.State != "" && !ok {
		return nil, errors.New("invalid state")
	}
	r.Title = strings.TrimSpace(r.Title)
	if len(r.Title) > 1024 {
		return nil, errors.New("invalid title")
	}
	tenant := strconv.FormatUint(r.TenantID, 10)
	lifecycleInconsistent := false
	err := s.db.View(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT c.state,count(*) FROM ext_lifecycle_current c `+store.LifecycleEventJoinSQL+` WHERE c.tenant_id=? AND c.knowledge_base_id=? AND `+store.LifecycleCurrentVerifiedSQL+` GROUP BY c.state`, tenant, r.KnowledgeBaseID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var state string
			var n int
			if err = rows.Scan(&state, &n); err != nil {
				rows.Close()
				return err
			}
			if _, ok := counts[state]; !ok {
				rows.Close()
				return errors.New("unknown lifecycle state")
			}
			counts[state] = n
		}
		if err = rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
		var invalid int
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM ext_lifecycle_current c `+store.LifecycleEventJoinSQL+` WHERE c.tenant_id=? AND c.knowledge_base_id=? AND NOT (`+store.LifecycleCurrentVerifiedSQL+`)`, tenant, r.KnowledgeBaseID).Scan(&invalid); err != nil {
			return err
		}
		lifecycleInconsistent = invalid > 0
		return nil
	})
	if err != nil {
		return nil, errDatabase
	}
	ids, total, nativeDegraded, err := s.overviewPage(ctx, r, tenant)
	if err != nil {
		return nil, errDatabase
	}
	if !nativeDegraded && s.nativeDB != "" {
		// The workbench list follows native knowledges; state counts must use
		// the same membership instead of the older extension-only projection.
		nativeCounts, countErr := s.nativeOverviewCounts(ctx, tenant, r.KnowledgeBaseID)
		if countErr != nil {
			return nil, errDatabase
		}
		for state := range counts {
			counts[state] = 0
		}
		for state, n := range nativeCounts {
			counts[state] = n
		}
	}
	degraded := nativeDegraded || lifecycleInconsistent
	// Bounded page snapshots reuse the canonical date decoder and scoped policy read.
	if err := s.enrichDocumentFamilies(ctx, r, ids); err != nil {
		return nil, errDatabase
	}
	m := lifecycle.NewMachine(s.db, nil)
	documentIDs := make([]string, len(ids))
	for i := range ids {
		documentIDs[i] = ids[i].DocumentID
	}
	annotations, batchErr := m.AnnotationsForDocuments(ctx, lifecycle.Scope{TenantID: r.TenantID, KnowledgeBaseID: r.KnowledgeBaseID}, documentIDs)
	if batchErr == nil {
		for i := range ids {
			ids[i].Annotations = annotations[ids[i].DocumentID]
		}
	} else {
		// Preserve per-document degradation if a malformed annotation still
		// prevents a batch read.
		for i := range ids {
			snap, err := m.Snapshot(ctx, lifecycle.Scope{TenantID: r.TenantID, KnowledgeBaseID: r.KnowledgeBaseID, DocumentID: ids[i].DocumentID})
			if err != nil {
				ids[i].Degraded = true
				ids[i].DegradedReason = "lifecycle_inconsistent"
				degraded = true
				continue
			}
			ids[i].Annotations = snap.Annotations
		}
	}
	// Attachments carry a generic native title (annex-N.docx). Overlay the
	// recognised semantic name for display only; native title and permissions
	// stay untouched.
	s.enrichAttachmentTitles(ctx, r, ids)
	rules, err := s.rules(ctx, r)
	if err != nil {
		return nil, err
	}
	if summary := s.cleanupSummary(ctx, r); summary != nil {
		rules["cleanup"] = summary
	}
	rules["counts"] = counts
	rules["total"] = total
	rules["page"] = r.Page
	rules["page_size"] = r.PageSize
	rules["documents"] = ids
	rules["can_write"] = r.CanWrite
	rules["degraded"] = degraded
	if degraded {
		switch {
		case nativeDegraded && lifecycleInconsistent:
			rules["degraded_reason"] = "native_database_unavailable,lifecycle_inconsistent"
		case lifecycleInconsistent:
			rules["degraded_reason"] = "lifecycle_inconsistent"
		default:
			rules["degraded_reason"] = "native_database_unavailable"
		}
	}
	return rules, nil
}
func (s *Server) events(ctx context.Context, scope lifecycle.Scope) ([]lifecycle.Event, bool, error) {
	events := []lifecycle.Event{}
	err := s.db.View(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT payload_json FROM ext_lifecycle_events WHERE tenant_id=? AND knowledge_base_id=? AND document_id=? ORDER BY sequence DESC LIMIT 101`, strconv.FormatUint(scope.TenantID, 10), scope.KnowledgeBaseID, scope.DocumentID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var raw []byte
			var event lifecycle.Event
			if err = rows.Scan(&raw); err != nil {
				return err
			}
			if err = json.Unmarshal(raw, &event); err != nil {
				return err
			}
			events = append(events, event)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, false, errDatabase
	}
	truncated := len(events) > 100
	if truncated {
		events = events[:100]
	}
	return events, truncated, nil
}
