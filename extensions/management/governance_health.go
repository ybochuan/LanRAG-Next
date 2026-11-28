package management

import (
	"context"
	"database/sql"
	"strconv"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/outbox"
)

// TagHealth reports canonical tags absent from a native KB. Implementations must
// be read-only and must not create tags.
type TagHealth interface {
	MissingLifecycleStateTags(context.Context, uint64, string) ([]string, error)
}

type tagHealthView struct {
	Ready   bool     `json:"ready"`
	Missing []string `json:"missing"`
	Error   string   `json:"error,omitempty"`
}

type outboxHealthView struct {
	Pending        int   `json:"pending"`
	Processing     int   `json:"processing"`
	Dead           int   `json:"dead"`
	Blocked        int   `json:"blocked"`
	OldestPendingS int64 `json:"oldest_pending_seconds"`
}

type tagFailureView struct {
	Total          int64  `json:"total"`
	LastReason     string `json:"last_reason,omitempty"`
	LastOccurredAt string `json:"last_occurred_at,omitempty"`
}

// governanceHealth returns only counts and tag names needed by the workbench.
func (s *Server) governanceHealth(ctx context.Context, r Request) (any, error) {
	missing := []string{}
	tagState := tagHealthView{Missing: missing}
	if s.tagHealth == nil {
		tagState.Error = "tag_health_unavailable"
	} else if values, err := s.tagHealth.MissingLifecycleStateTags(ctx, r.TenantID, r.KnowledgeBaseID); err != nil {
		tagState.Error = "tag_catalog_unavailable"
	} else {
		tagState.Missing = values
		tagState.Ready = len(values) == 0
	}
	stats, err := outbox.NewStore(s.db).Stats(ctx, outbox.Scope{TenantID: r.TenantID, KnowledgeBaseID: r.KnowledgeBaseID}, time.Now().UTC())
	if err != nil {
		return nil, errDatabase
	}
	failures := tagFailureView{}
	if err := s.db.View(ctx, func(tx *sql.Tx) error {
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM ext_tag_sync_failures WHERE tenant_id=? AND knowledge_base_id=?`,
			strconv.FormatUint(r.TenantID, 10), r.KnowledgeBaseID).Scan(&failures.Total); err != nil {
			return err
		}
		var reason string
		var occurred int64
		err := tx.QueryRowContext(ctx, `SELECT reason,occurred_at FROM ext_tag_sync_failures WHERE tenant_id=? AND knowledge_base_id=? ORDER BY occurred_at DESC,failure_id DESC LIMIT 1`,
			strconv.FormatUint(r.TenantID, 10), r.KnowledgeBaseID).Scan(&reason, &occurred)
		if err == sql.ErrNoRows {
			return nil
		}
		if err != nil {
			return err
		}
		failures.LastReason = reason
		failures.LastOccurredAt = time.UnixMilli(occurred).UTC().Format(time.RFC3339)
		return nil
	}); err != nil {
		return nil, errDatabase
	}
	return map[string]any{
		"knowledge_base_id": r.KnowledgeBaseID,
		"tags":              tagState,
		"outbox": outboxHealthView{
			Pending: stats.Pending, Processing: stats.Processing, Dead: stats.Dead, Blocked: stats.Blocked,
			OldestPendingS: int64(stats.OldestPendingAge / time.Second),
		},
		"tag_failures": failures,
	}, nil
}
