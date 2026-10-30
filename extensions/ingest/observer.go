package ingest

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/store"
	"github.com/ybochuan/LanRAG-Next/extensions/weknora"
)

var (
	// ErrStaleObservation prevents an older source snapshot overwriting a newer one.
	ErrStaleObservation = errors.New("source snapshot is older than persisted observation")
	// ErrObservationConflict exposes contradictory states at the same source timestamp.
	ErrObservationConflict = errors.New("source timestamp has conflicting processing states")
)

// PersistentObserver stores reconcile observations separately from lifecycle truth.
type PersistentObserver struct{ db *store.DB }

// NewObserver shares the database WriteExecutor and does not own its lifetime.
func NewObserver(db *store.DB) *PersistentObserver { return &PersistentObserver{db} }

func observationArgs(scope Scope, docID string) []any {
	return []any{strconv.FormatUint(scope.TenantID, 10), scope.KnowledgeBaseID, docID}
}

func validObservationScope(scope Scope, docID string) bool {
	return scope.TenantID != 0 && weknora.ValidID(scope.KnowledgeBaseID) && weknora.ValidID(docID)
}

const currentQuery = `SELECT o.parse_status,o.source_updated_at,c.last_seen_at,o.origin
FROM ext_ingest_current c JOIN ext_ingest_observations o ON o.observation_id=c.last_observation_id
WHERE c.tenant_id=? AND c.knowledge_base_id=? AND c.document_id=?`

func readObservation(row *sql.Row, scope Scope, docID string) (Observation, error) {
	result := Observation{Snapshot: Snapshot{Scope: scope, DocumentID: docID}}
	var source string
	var seen int64
	if err := row.Scan(&result.ParseStatus, &source, &seen, &result.Origin); err != nil {
		return result, err
	}
	var err error
	result.UpdatedAt, err = time.Parse(time.RFC3339Nano, source)
	result.ObservedAt = time.UnixMilli(seen).UTC()
	return result, err
}

// Observe commits the observed snapshot and current pointer together. It records
// no invented pipeline event, lifecycle transition, or projection intent.
func (s *PersistentObserver) Observe(ctx context.Context, obs Observation) error {
	if !validObservationScope(obs.Scope, obs.DocumentID) || strings.TrimSpace(obs.ParseStatus) == "" || obs.UpdatedAt.IsZero() || obs.ObservedAt.IsZero() || obs.Origin != "reconcile" {
		return errors.New("invalid reconcile observation")
	}
	obs.UpdatedAt, obs.ObservedAt = obs.UpdatedAt.UTC(), obs.ObservedAt.UTC()
	return s.db.Write(ctx, func(tx *sql.Tx) error {
		args := observationArgs(obs.Scope, obs.DocumentID)
		current, err := readObservation(tx.QueryRowContext(ctx, currentQuery, args...), obs.Scope, obs.DocumentID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err == nil {
			if obs.UpdatedAt.Before(current.UpdatedAt) {
				return ErrStaleObservation
			}
			if obs.UpdatedAt.Equal(current.UpdatedAt) {
				if obs.ParseStatus != current.ParseStatus {
					return ErrObservationConflict
				}
				_, err := tx.ExecContext(ctx, "UPDATE ext_ingest_current SET last_seen_at=MAX(last_seen_at,?) WHERE tenant_id=? AND knowledge_base_id=? AND document_id=?", append([]any{obs.ObservedAt.UnixMilli()}, args...)...)
				return err
			}
		}
		result, err := tx.ExecContext(ctx, `INSERT INTO ext_ingest_observations(tenant_id,knowledge_base_id,document_id,parse_status,source_updated_at,observed_at,origin) VALUES(?,?,?,?,?,?,'reconcile')`, append(args, obs.ParseStatus, obs.UpdatedAt.Format(time.RFC3339Nano), obs.ObservedAt.UnixMilli())...)
		if err != nil {
			return err
		}
		id, err := result.LastInsertId()
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO ext_ingest_current(tenant_id,knowledge_base_id,document_id,last_observation_id,last_seen_at) VALUES(?,?,?,?,?)
		ON CONFLICT(tenant_id,knowledge_base_id,document_id) DO UPDATE SET last_observation_id=excluded.last_observation_id,last_seen_at=MAX(last_seen_at,excluded.last_seen_at)`, append(observationArgs(obs.Scope, obs.DocumentID), id, obs.ObservedAt.UnixMilli())...)
		return err
	})
}

// Current returns the latest source snapshot and most recent observation time.
func (s *PersistentObserver) Current(ctx context.Context, scope Scope, docID string) (Observation, error) {
	var result Observation
	if !validObservationScope(scope, docID) {
		return result, errors.New("invalid observation scope")
	}
	err := s.db.View(ctx, func(tx *sql.Tx) error {
		var err error
		result, err = readObservation(tx.QueryRowContext(ctx, currentQuery, observationArgs(scope, docID)...), scope, docID)
		return err
	})
	return result, err
}
