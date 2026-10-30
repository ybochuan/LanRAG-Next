// Package outbox persists projection intent and leases independently of upstream data.
package outbox

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/store"
)

// ErrLeaseLost means a worker no longer owns a live processing lease.
var ErrLeaseLost = errors.New("outbox lease expired or ownership changed")

// Scope limits administrative reads and manual retries to one tenant/KB.
type Scope struct {
	TenantID        uint64
	KnowledgeBaseID string
}

// Item is one causation-based projection. Timestamps use UTC; leases fence local
// acknowledgements, not remote side effects. Handlers must reconcile current truth.
type Item struct {
	ID              string
	TenantID        uint64
	KnowledgeBaseID string
	ProjectionType  string
	AggregateType   string
	AggregateID     string
	CausationType   string
	CausationID     string
	Payload         json.RawMessage
	Status          string
	RetryCount      int
	NextRetryAt     time.Time
	LastError       string
	CreatedAt       time.Time
	ProcessedAt     time.Time
	LeaseToken      string
	LeaseUntil      time.Time
}

// Stats exposes actionable queue health, including expired processing items.
type Stats struct {
	Pending          int
	Processing       int
	Dead             int
	OldestPendingAge time.Duration
}

// Store shares the extension database and its single writer.
type Store struct{ db *store.DB }

// NewStore does not own or close db.
func NewStore(db *store.DB) *Store { return &Store{db: db} }

func (scope Scope) validate() error {
	if scope.TenantID == 0 || scope.KnowledgeBaseID == "" {
		return errors.New("complete outbox scope is required")
	}
	return nil
}

// EnqueueTx must be called in the same transaction as the causal business fact.
// A constraint conflict is returned to the producer; it is never silently ignored.
func EnqueueTx(ctx context.Context, tx *sql.Tx, item Item) error {
	if err := (Scope{item.TenantID, item.KnowledgeBaseID}).validate(); err != nil {
		return err
	}
	if item.ID == "" || item.ProjectionType == "" || item.AggregateType == "" || item.AggregateID == "" || item.CausationType == "" || item.CausationID == "" || item.CreatedAt.IsZero() || !json.Valid(item.Payload) {
		return errors.New("incomplete outbox item")
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO ext_projection_outbox
		(outbox_id,tenant_id,knowledge_base_id,projection_type,aggregate_type,aggregate_id,causation_type,causation_id,payload_json,status,next_retry_at,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,'pending',?,?)`, item.ID, strconv.FormatUint(item.TenantID, 10), item.KnowledgeBaseID, item.ProjectionType,
		item.AggregateType, item.AggregateID, item.CausationType, item.CausationID, string(item.Payload), item.CreatedAt.UnixMilli(), item.CreatedAt.UnixMilli())
	return err
}

const columns = `outbox_id,tenant_id,knowledge_base_id,projection_type,aggregate_type,aggregate_id,causation_type,causation_id,
payload_json,status,retry_count,next_retry_at,last_error,created_at,processed_at,lease_token,lease_until`

func scanItem(row interface{ Scan(...any) error }) (*Item, error) {
	var item Item
	var tenant string
	var payload string
	var next, created int64
	var processed, lease sql.NullInt64
	err := row.Scan(&item.ID, &tenant, &item.KnowledgeBaseID, &item.ProjectionType, &item.AggregateType, &item.AggregateID,
		&item.CausationType, &item.CausationID, &payload, &item.Status, &item.RetryCount, &next, &item.LastError, &created, &processed, &item.LeaseToken, &lease)
	if err != nil {
		return nil, err
	}
	item.TenantID, err = strconv.ParseUint(tenant, 10, 64)
	if err != nil {
		return nil, err
	}
	item.Payload = json.RawMessage(payload)
	item.CreatedAt, item.NextRetryAt = time.UnixMilli(created).UTC(), time.UnixMilli(next).UTC()
	if processed.Valid {
		item.ProcessedAt = time.UnixMilli(processed.Int64).UTC()
	}
	if lease.Valid {
		item.LeaseUntil = time.UnixMilli(lease.Int64).UTC()
	}
	return &item, nil
}

// Claim atomically acquires one due pending item or reclaims an expired lease.
func (s *Store) Claim(ctx context.Context, now time.Time, lease time.Duration) (*Item, error) {
	return s.claim(ctx, now, lease, "", nil)
}

// ClaimScoped restricts pending and expired processing work before acquisition.
// An empty allowlist is an error, never a request for all tenants/KBs.
func (s *Store) ClaimScoped(ctx context.Context, now time.Time, lease time.Duration, scopes []Scope) (*Item, error) {
	if len(scopes) == 0 || len(scopes) > 1000 {
		return nil, errors.New("claim requires 1..1000 explicit scopes")
	}
	marks := make([]string, 0, len(scopes))
	args := make([]any, 0, 2*len(scopes))
	seen := make(map[Scope]bool)
	for _, scope := range scopes {
		if err := scope.validate(); err != nil {
			return nil, err
		}
		if seen[scope] {
			return nil, errors.New("duplicate claim scope")
		}
		seen[scope] = true
		marks = append(marks, "(?,?)")
		args = append(args, strconv.FormatUint(scope.TenantID, 10), scope.KnowledgeBaseID)
	}
	return s.claim(ctx, now, lease, " AND (tenant_id,knowledge_base_id) IN ("+strings.Join(marks, ",")+")", args)
}

func (s *Store) claim(ctx context.Context, now time.Time, lease time.Duration, scopeSQL string, scopeArgs []any) (*Item, error) {
	if now.IsZero() || lease < time.Millisecond {
		return nil, errors.New("valid claim time and lease >= 1ms required")
	}
	var token [16]byte
	if _, err := rand.Read(token[:]); err != nil {
		return nil, err
	}
	var item *Item
	err := s.db.Write(ctx, func(tx *sql.Tx) error {
		var err error
		item, err = scanItem(tx.QueryRowContext(ctx, `SELECT `+columns+` FROM ext_projection_outbox
			WHERE ((status='pending' AND next_retry_at<=?) OR (status='processing' AND lease_until<=?))`+scopeSQL+`
			ORDER BY created_at,outbox_id LIMIT 1`, append([]any{now.UnixMilli(), now.UnixMilli()}, scopeArgs...)...))
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		item.Status, item.LeaseToken, item.LeaseUntil = "processing", hex.EncodeToString(token[:]), now.Add(lease).UTC()
		_, err = tx.ExecContext(ctx, `UPDATE ext_projection_outbox SET status='processing',lease_token=?,lease_until=? WHERE outbox_id=?`, item.LeaseToken, item.LeaseUntil.UnixMilli(), item.ID)
		return err
	})
	if err != nil {
		return nil, err
	}
	return item, nil
}

func leaseUpdate(result sql.Result, err error) error {
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrLeaseLost
	}
	return nil
}

// Complete acknowledges only the matching unexpired processing lease.
func (s *Store) Complete(ctx context.Context, id, token string, now time.Time) error {
	if id == "" || token == "" || now.IsZero() {
		return ErrLeaseLost
	}
	return s.db.Write(ctx, func(tx *sql.Tx) error {
		return leaseUpdate(tx.ExecContext(ctx, `UPDATE ext_projection_outbox SET status='done',processed_at=?,lease_token='',lease_until=NULL
			WHERE outbox_id=? AND status='processing' AND lease_token=? AND lease_until>?`, now.UnixMilli(), id, token, now.UnixMilli()))
	})
}

// Fail durably schedules a retry, or marks dead after maxAttempts failed deliveries.
// An expired owner may record failure until another claim changes the token;
// otherwise a handler reaching its own deadline could never count an attempt.
func (s *Store) Fail(ctx context.Context, id, token string, now time.Time, failure error, retryDelay time.Duration, maxAttempts int) error {
	if failure == nil || retryDelay < time.Millisecond || maxAttempts < 1 || now.IsZero() {
		return errors.New("valid failure/retry configuration required")
	}
	if id == "" || token == "" {
		return ErrLeaseLost
	}
	message := []rune(failure.Error())
	if len(message) > 2048 {
		message = message[:2048]
	}
	return s.db.Write(ctx, func(tx *sql.Tx) error {
		return leaseUpdate(tx.ExecContext(ctx, `UPDATE ext_projection_outbox SET retry_count=retry_count+1,
			status=CASE WHEN retry_count+1>=? THEN 'dead' ELSE 'pending' END,next_retry_at=?,last_error=?,
			processed_at=NULL,lease_token='',lease_until=NULL WHERE outbox_id=? AND status='processing' AND lease_token=?`,
			maxAttempts, now.Add(retryDelay).UnixMilli(), string(message), id, token))
	})
}

// RetryDead explicitly resets a scoped dead item; it cannot revive processing/done.
func (s *Store) RetryDead(ctx context.Context, scope Scope, id string, now time.Time) error {
	if err := scope.validate(); err != nil {
		return err
	}
	if id == "" || now.IsZero() {
		return errors.New("retry ID and time required")
	}
	return s.db.Write(ctx, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `UPDATE ext_projection_outbox SET status='pending',retry_count=0,next_retry_at=?,last_error='',processed_at=NULL
			WHERE tenant_id=? AND knowledge_base_id=? AND outbox_id=? AND status='dead'`, now.UnixMilli(), strconv.FormatUint(scope.TenantID, 10), scope.KnowledgeBaseID, id)
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if n != 1 {
			return sql.ErrNoRows
		}
		return nil
	})
}

// ListDead returns a bounded scoped list for an authenticated operations adapter.
func (s *Store) ListDead(ctx context.Context, scope Scope, limit int) ([]Item, error) {
	if err := scope.validate(); err != nil {
		return nil, err
	}
	if limit < 1 || limit > 1000 {
		return nil, errors.New("dead list limit must be 1..1000")
	}
	items := make([]Item, 0)
	err := s.db.View(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT `+columns+` FROM ext_projection_outbox WHERE tenant_id=? AND knowledge_base_id=? AND status='dead' ORDER BY created_at,outbox_id LIMIT ?`, strconv.FormatUint(scope.TenantID, 10), scope.KnowledgeBaseID, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			item, err := scanItem(rows)
			if err != nil {
				return err
			}
			items = append(items, *item)
		}
		return rows.Err()
	})
	return items, err
}

// Stats includes processing age so a crashed worker cannot hide an old backlog.
func (s *Store) Stats(ctx context.Context, scope Scope, now time.Time) (Stats, error) {
	var stats Stats
	if err := scope.validate(); err != nil {
		return stats, err
	}
	if now.IsZero() {
		return stats, errors.New("statistics time required")
	}
	err := s.db.View(ctx, func(tx *sql.Tx) error {
		var oldest sql.NullInt64
		err := tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(status='pending'),0),COALESCE(SUM(status='processing'),0),COALESCE(SUM(status='dead'),0),
			MIN(CASE WHEN status IN ('pending','processing') THEN created_at END) FROM ext_projection_outbox WHERE tenant_id=? AND knowledge_base_id=?`,
			strconv.FormatUint(scope.TenantID, 10), scope.KnowledgeBaseID).Scan(&stats.Pending, &stats.Processing, &stats.Dead, &oldest)
		if err != nil {
			return err
		}
		if oldest.Valid {
			stats.OldestPendingAge = max(time.Duration(0), now.Sub(time.UnixMilli(oldest.Int64)))
		}
		return nil
	})
	return stats, err
}
