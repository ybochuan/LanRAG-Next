package management

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"

	"github.com/ybochuan/LanRAG-Next/extensions/lifecycle"
)

type document struct {
	DocumentID  string                `json:"document_id"`
	State       string                `json:"state"`
	LastEventID string                `json:"last_event_id"`
	Annotations lifecycle.Annotations `json:"annotations"`
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
	ids := []document{}
	total := 0
	tenant := strconv.FormatUint(r.TenantID, 10)
	err := s.db.View(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, "SELECT state,count(*) FROM ext_lifecycle_current WHERE tenant_id=? AND knowledge_base_id=? GROUP BY state", tenant, r.KnowledgeBaseID)
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
			counts[state] = n
			if r.State == "" || r.State == state {
				total += n
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		rows, err = tx.QueryContext(ctx, `SELECT document_id,state,last_event_id FROM ext_lifecycle_current WHERE tenant_id=? AND knowledge_base_id=? AND (?='' OR state=?) ORDER BY document_id LIMIT ? OFFSET ?`, tenant, r.KnowledgeBaseID, r.State, r.State, r.PageSize, (r.Page-1)*r.PageSize)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var d document
			if err = rows.Scan(&d.DocumentID, &d.State, &d.LastEventID); err != nil {
				return err
			}
			ids = append(ids, d)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, errDatabase
	}
	// Bounded page snapshots reuse the canonical date decoder and scoped policy read.
	m := lifecycle.NewMachine(s.db, nil)
	for i := range ids {
		snap, err := m.Snapshot(ctx, lifecycle.Scope{TenantID: r.TenantID, KnowledgeBaseID: r.KnowledgeBaseID, DocumentID: ids[i].DocumentID})
		if err != nil {
			return nil, errDatabase
		}
		ids[i].Annotations = snap.Annotations
	}
	rules, err := s.rules(ctx, r)
	if err != nil {
		return nil, err
	}
	rules["counts"] = counts
	rules["total"] = total
	rules["page"] = r.Page
	rules["page_size"] = r.PageSize
	rules["documents"] = ids
	rules["can_write"] = r.CanWrite
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
