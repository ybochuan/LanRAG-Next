package store

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
)

// ErrLifecycleInconsistent refuses to treat a corrupted projection as a new fact.
var ErrLifecycleInconsistent = errors.New("lifecycle current does not match latest event; explicit repair required")

// LifecycleEventJoinSQL binds the aliases c (current) and e (event) in scoped queries.
const LifecycleEventJoinSQL = ` LEFT JOIN ext_lifecycle_events e ON e.tenant_id=c.tenant_id AND e.knowledge_base_id=c.knowledge_base_id AND e.document_id=c.document_id AND e.event_id=c.last_event_id `

// LifecycleCurrentVerifiedSQL checks existing rows as well as new writes guarded
// by schema7. It must be evaluated in the same snapshot as the permission decision.
const LifecycleCurrentVerifiedSQL = `c.state=e.to_state AND NOT EXISTS (SELECT 1 FROM ext_lifecycle_events newer WHERE newer.tenant_id=c.tenant_id AND newer.knowledge_base_id=c.knowledge_base_id AND newer.document_id=c.document_id AND newer.sequence>e.sequence)`

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
