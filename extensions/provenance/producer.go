package provenance

import (
	"context"
	"database/sql"
	"encoding/json"
	"path"
	"strings"
)

// ProducerRequest binds the intended whole-byte write to its original run and step.
type ProducerRequest struct {
	OperationID string      `json:"operation_id"`
	StepID      string      `json:"step_id"`
	CopyID      string      `json:"copy_id"`
	Path        string      `json:"path"`
	Binding     Binding     `json:"binding"`
	Parents     []ParentRef `json:"parents"`
}

// ProducerHandle can be reconciled by exact reservation retry, without rewriting files.
type ProducerHandle struct {
	ProducerID string `json:"producer_id"`
}

// ProducerResult compares actual read-back bytes with the reserved whole-byte binding.
type ProducerResult struct {
	OperationID string  `json:"operation_id"`
	ProducerID  string  `json:"producer_id"`
	StepID      string  `json:"step_id"`
	Binding     Binding `json:"binding"`
}

type producerFact struct {
	Request    ProducerRequest `json:"request"`
	ProducerID string          `json:"producer_id"`
	RunID      string          `json:"run_id"`
	Inherited  []SourceRef     `json:"inherited"`
}

func canonicalProducerPath(value string) bool {
	return value != "" && len(value) <= 1024 && value == path.Clean(value) && value != "." && value != ".." && !strings.HasPrefix(value, "../") && !strings.HasPrefix(value, "/") && !strings.ContainsAny(value, "\\\x00\r\n:")
}

// ReserveProducer does not attest existing files. Only an actual acknowledged
// write with matching final bytes can make the reservation committed.
func (r *Runs) ReserveProducer(ctx context.Context, h RunHandle, q ProducerRequest) (p ProducerHandle, err error) {
	if !id(q.StepID) || !id(q.CopyID) || !canonicalProducerPath(q.Path) || !validBinding(q.Binding) || len(q.Parents) > 16 {
		return p, ErrInvalid
	}
	v, e := r.Inspect(ctx, h)
	if e != nil {
		return p, e
	}
	if q.Binding.UserID != v.Identity.UserID || q.Binding.SessionID != v.Identity.SessionID {
		return p, ErrEvidence
	}
	if exists, e := r.replayMutation(ctx, h, q.OperationID, "producer-reserve", q, &p); e != nil || exists {
		return p, e
	}
	m := Manifest{Scope: h.Scope, Binding: q.Binding, Parents: q.Parents, Sources: v.Sources}
	prepared, e := r.ledger.preparePermission(ctx, m)
	if e != nil {
		return p, ErrEvidence
	}
	err = r.mutate(ctx, h, q.OperationID, "producer-reserve", q, &p, func(tx *sql.Tx, v *RunSnapshot) error {
		kind, e := stepOpen(ctx, tx, h, q.StepID)
		if e != nil || kind != "write" {
			return ErrConflict
		}
		fact := producerFact{Request: q, RunID: h.RunID, Inherited: []SourceRef{}}
		seen := map[ParentRef]bool{}
		for _, parent := range q.Parents {
			if !id(parent.CopyID) || parent.Revision <= 0 || seen[parent] || parent.CopyID == q.CopyID {
				return ErrInvalid
			}
			seen[parent] = true
			pm, e := loadManifest(ctx, tx, h.Scope, parent.CopyID, 0)
			if e != nil || pm.Revision != parent.Revision || pm.Binding.UserID != q.Binding.UserID || pm.Binding.SessionID != q.Binding.SessionID {
				return ErrEvidence
			}
			if reason, e := r.ledger.checkManifest(ctx, tx, pm); e != nil || reason != "" {
				return ErrEvidence
			}
			fact.Inherited, e = sourceUnion(fact.Inherited, pm.Sources)
			if e != nil {
				return e
			}
		}
		if len(fact.Inherited) > 0 {
			if e := r.ledger.verifyPermissionTx(ctx, tx, h.Scope, fact.Inherited, prepared); e != nil {
				return ErrEvidence
			}
			v.Sources, e = sourceUnion(v.Sources, fact.Inherited)
			if e != nil {
				return e
			}
		}
		if len(v.Sources) == 0 || r.ledger.verifyPermissionTx(ctx, tx, h.Scope, v.Sources, prepared) != nil {
			return ErrEvidence
		}
		for _, ref := range v.Sources {
			if reason, e := r.ledger.sourceReason(ctx, tx, h.Scope, ref); e != nil || reason != "" {
				return ErrEvidence
			}
		}
		p.ProducerID, e = randomFactID()
		if e != nil {
			return e
		}
		fact.ProducerID = p.ProducerID
		body, _ := json.Marshal(fact)
		_, e = tx.ExecContext(ctx, "INSERT INTO ext_provenance_producers VALUES(?,?,?,?,?,'reserved',?)", args(h.Scope, h.RunID, q.StepID, p.ProducerID, string(body))...)
		return e
	})
	if err != nil {
		return ProducerHandle{}, err
	}
	return
}

func loadProducer(ctx context.Context, tx *sql.Tx, s Scope, producerID string) (fact producerFact, state string, err error) {
	var body []byte
	err = tx.QueryRowContext(ctx, "SELECT payload_json,state FROM ext_provenance_producers WHERE tenant_id=? AND knowledge_base_id=? AND producer_id=?", args(s, producerID)...).Scan(&body, &state)
	if err == nil && (json.Unmarshal(body, &fact) != nil || fact.ProducerID != producerID || !validBinding(fact.Request.Binding)) {
		err = ErrUnavailable
	}
	return
}

// CommitProducer is only a fact ACK. Never retry the underlying write on ACK loss.
func (r *Runs) CommitProducer(ctx context.Context, h RunHandle, q ProducerResult) error {
	if !id(q.ProducerID) || !id(q.StepID) || !validBinding(q.Binding) {
		return ErrInvalid
	}
	var result struct{}
	err := r.mutate(ctx, h, q.OperationID, "producer-commit", q, &result, func(tx *sql.Tx, v *RunSnapshot) error {
		p, state, e := loadProducer(ctx, tx, h.Scope, q.ProducerID)
		if e != nil || state != "reserved" || p.RunID != h.RunID || p.Request.StepID != q.StepID || p.Request.Binding != q.Binding {
			return ErrEvidence
		}
		if _, e := stepOpen(ctx, tx, h, q.StepID); e != nil {
			return e
		}
		_, e = tx.ExecContext(ctx, "UPDATE ext_provenance_producers SET state='committed' WHERE tenant_id=? AND knowledge_base_id=? AND producer_id=?", args(h.Scope, q.ProducerID)...)
		return e
	})
	if err == ErrEvidence {
		r.taintStep(ctx, h, q.StepID, q.OperationID)
	}
	return err
}

// SealedCopy implements lineage.SealedCopyReader without importing lineage.
// The original producer and final sealed run must agree on owner/session/bytes.
func (r *Runs) SealedCopy(ctx context.Context, s Scope, copyID string, b Binding, evidence string) (proof CopyProof, err error) {
	if !r.available() {
		return proof, ErrUnavailable
	}
	if !validScope(s) || !id(copyID) || !validBinding(b) || !id(evidence) {
		return proof, ErrInvalid
	}
	err = r.db.View(ctx, func(tx *sql.Tx) error {
		p, state, e := loadProducer(ctx, tx, s, evidence)
		if e != nil || state != "committed" || p.Request.CopyID != copyID || p.Request.Binding != b {
			return ErrEvidence
		}
		v, _, e := loadRun(ctx, tx, s, p.RunID)
		if e != nil || v.State != "sealed" || v.Identity.UserID != b.UserID || v.Identity.SessionID != b.SessionID || len(v.Sources) == 0 {
			return ErrEvidence
		}
		var stepState string
		if e = tx.QueryRowContext(ctx, "SELECT state FROM ext_provenance_steps WHERE tenant_id=? AND knowledge_base_id=? AND run_id=? AND step_id=?", args(s, p.RunID, p.Request.StepID)...).Scan(&stepState); e != nil || stepState != "success" {
			return ErrEvidence
		}
		proof = CopyProof{Scope: s, CopyID: copyID, Binding: b, EvidenceID: evidence, EvidenceHash: payloadHash("sealed-producer-v1", struct {
			Run      RunSnapshot
			Producer producerFact
		}{v, p}), RunID: p.RunID, Sources: v.Sources, Parents: p.Request.Parents}
		return nil
	})
	if err != nil {
		return CopyProof{}, err
	}
	return
}
