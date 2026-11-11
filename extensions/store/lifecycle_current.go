package store

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"time"
)

// ErrLifecycleInconsistent refuses to treat a corrupted projection as a new fact.
var ErrLifecycleInconsistent = errors.New("lifecycle current does not match latest event; explicit repair required")

// LifecycleEventJoinSQL binds the aliases c (current) and e (event) in scoped queries.
const LifecycleEventJoinSQL = ` LEFT JOIN ext_lifecycle_events e ON e.tenant_id=c.tenant_id AND e.knowledge_base_id=c.knowledge_base_id AND e.document_id=c.document_id AND e.event_id=c.last_event_id `

// LifecycleAnnotationJoinSQL binds the optional dates for the same current row.
const LifecycleAnnotationJoinSQL = ` LEFT JOIN ext_document_annotations a ON a.tenant_id=c.tenant_id AND a.knowledge_base_id=c.knowledge_base_id AND a.document_id=c.document_id `

// EffectiveAt checks persisted RFC3339 bounds, preserving the state machine's
// inclusive deadline. Missing bounds are open and malformed bounds never allow.
func EffectiveAt(now time.Time, from, until sql.NullString) (bool, error) {
	if from.Valid {
		start, err := time.Parse(time.RFC3339Nano, from.String)
		if err != nil || now.Before(start) {
			return false, err
		}
	}
	if until.Valid {
		end, err := time.Parse(time.RFC3339Nano, until.String)
		if err != nil || now.After(end) {
			return false, err
		}
	}
	return true, nil
}

// LifecycleCurrentVerifiedSQL checks existing rows as well as new writes guarded
// by schema7. It must be evaluated in the same snapshot as the permission decision.
const LifecycleCurrentVerifiedSQL = `c.state=e.to_state AND NOT EXISTS (SELECT 1 FROM ext_lifecycle_events newer WHERE newer.tenant_id=c.tenant_id AND newer.knowledge_base_id=c.knowledge_base_id AND newer.document_id=c.document_id AND newer.sequence>e.sequence)`

// RepealDueSQL identifies an approved target awaiting its due transition.
// The alias c is the scoped lifecycle-current row.
const RepealDueSQL = `EXISTS(SELECT 1 FROM ext_repeal_notices r
	WHERE r.tenant_id=c.tenant_id AND r.knowledge_base_id=c.knowledge_base_id AND r.target_document_id=c.document_id
	AND r.status='scheduled' AND r.effective_at_ms<=CAST(unixepoch('subsec')*1000 AS INTEGER))`

// LifecycleReadableSQL preserves the current fact while a due repeal is verified.
const LifecycleReadableSQL = "(" + LifecycleCurrentVerifiedSQL + ") AND NOT " + RepealDueSQL

// ReadLifecycleCurrentTx distinguishes an absent document from an invalid stored
// projection. Callers must never initialize or revive the latter automatically.
func ReadLifecycleCurrentTx(ctx context.Context, tx *sql.Tx, tenant uint64, kb, doc string) (state, event string, err error) {
	var verified int
	err = tx.QueryRowContext(ctx, `SELECT c.state,c.last_event_id,CASE WHEN `+LifecycleCurrentVerifiedSQL+` THEN 1 ELSE 0 END FROM ext_lifecycle_current c `+LifecycleEventJoinSQL+` WHERE c.tenant_id=? AND c.knowledge_base_id=? AND c.document_id=?`, strconv.FormatUint(tenant, 10), kb, doc).Scan(&state, &event, &verified)
	if errors.Is(err, sql.ErrNoRows) {
		var hasHistory int
		if e := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM ext_lifecycle_events WHERE tenant_id=? AND knowledge_base_id=? AND document_id=?)", strconv.FormatUint(tenant, 10), kb, doc).Scan(&hasHistory); e != nil {
			return "", "", e
		}
		if hasHistory != 0 {
			return "", "", ErrLifecycleInconsistent
		}
	}
	if err == nil && verified != 1 {
		return "", "", ErrLifecycleInconsistent
	}
	return
}
