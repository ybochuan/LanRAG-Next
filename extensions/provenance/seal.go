package provenance

import (
	"context"
	"database/sql"
)

// FinishRequest is a CAS against the complete acknowledged run head.
type FinishRequest struct {
	OperationID      string `json:"operation_id"`
	ExpectedRevision int64  `json:"expected_revision"`
	Abort            bool   `json:"abort"`
}

// Finish seals only a nonempty governed input set with all operations terminal.
// A receipt is historical evidence, never permission to consume a source/copy.
func (r *Runs) Finish(ctx context.Context, h RunHandle, q FinishRequest) (result RunSnapshot, err error) {
	if q.ExpectedRevision <= 0 {
		return result, ErrInvalid
	}
	v, e := r.Inspect(ctx, h)
	if e != nil {
		return result, e
	}
	var prepared []SourceRef
	if !q.Abort && v.State == "open" {
		prepared, e = r.ledger.preparePermission(ctx, Manifest{Scope: h.Scope, Sources: v.Sources})
		if e != nil {
			return result, ErrEvidence
		}
	}
	err = r.mutate(ctx, h, q.OperationID, "finish", q, &result, func(tx *sql.Tx, v *RunSnapshot) error {
		if v.Revision != q.ExpectedRevision {
			return ErrConflict
		}
		if q.Abort {
			v.State = "aborted"
		} else {
			var pending, models, producers int
			if e := tx.QueryRowContext(ctx, "SELECT count(*) FROM ext_provenance_steps WHERE tenant_id=? AND knowledge_base_id=? AND run_id=? AND state<>'success'", args(h.Scope, h.RunID)...).Scan(&pending); e != nil {
				return e
			}
			if e := tx.QueryRowContext(ctx, "SELECT count(*) FROM ext_provenance_steps WHERE tenant_id=? AND knowledge_base_id=? AND run_id=? AND kind='model' AND state='success'", args(h.Scope, h.RunID)...).Scan(&models); e != nil {
				return e
			}
			if e := tx.QueryRowContext(ctx, "SELECT count(*) FROM ext_provenance_producers WHERE tenant_id=? AND knowledge_base_id=? AND run_id=? AND state<>'committed'", args(h.Scope, h.RunID)...).Scan(&producers); e != nil {
				return e
			}
			if pending != 0 || models == 0 || producers != 0 || len(v.Sources) == 0 {
				return ErrEvidence
			}
			if e := r.ledger.verifyPermissionTx(ctx, tx, h.Scope, v.Sources, prepared); e != nil {
				return ErrEvidence
			}
			for _, ref := range v.Sources {
				if reason, e := r.ledger.sourceReason(ctx, tx, h.Scope, ref); e != nil || reason != "" {
					return ErrEvidence
				}
			}
			v.State = "sealed"
		}
		result = *v
		result.Revision++
		return nil
	})
	if err != nil {
		return RunSnapshot{}, err
	}
	return
}
