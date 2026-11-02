package provenance

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/store"
)

// RunIdentity comes from native authorization, never model/tool parameters.
type RunIdentity struct {
	Scope                 Scope    `json:"scope"`
	UserID                string   `json:"user_id"`
	SessionID             string   `json:"session_id"`
	AttemptID             string   `json:"attempt_id"`
	AuthorizationKind     string   `json:"authorization_kind,omitempty"`
	AuthorizedDocumentIDs []string `json:"authorized_document_ids"`
}

// RunHandle is server-only. Capability must never enter prompts or logs.
type RunHandle struct {
	Scope      Scope  `json:"scope"`
	RunID      string `json:"run_id"`
	Capability string `json:"capability"`
}

// RunSnapshot is a persisted input union, not a read permit.
type RunSnapshot struct {
	Identity  RunIdentity `json:"identity"`
	RunID     string      `json:"run_id"`
	Revision  int64       `json:"revision"`
	State     string      `json:"state"`
	Sources   []SourceRef `json:"sources"`
	CreatedAt time.Time   `json:"created_at"`
	ExpiresAt time.Time   `json:"expires_at"`
}

// Runs owns durable trusted facts. Its key derives retry-stable capabilities;
// only capability digests are persisted. Rotation safely invalidates open handles.
type Runs struct {
	db       *store.DB
	verifier InputVerifier
	ledger   *Ledger
	key      []byte
	now      func() time.Time
}

// NewRuns requires a persistent service key of at least32 bytes and a real verifier.
func NewRuns(db *store.DB, verifier InputVerifier, authority Authority, key []byte) *Runs {
	return &Runs{db: db, verifier: verifier, ledger: New(db, authority), key: append([]byte(nil), key...), now: time.Now}
}

func randomFactID() (string, error) {
	var data [24]byte
	if _, e := rand.Read(data[:]); e != nil {
		return "", e
	}
	return hex.EncodeToString(data[:]), nil
}

func (r *Runs) capability(s Scope, runID string) string {
	h := hmac.New(sha256.New, r.key)
	h.Write([]byte(payloadHash("run-capability-v1", struct {
		Scope Scope
		RunID string
	}{s, runID})))
	return hex.EncodeToString(h.Sum(nil))
}

func (r *Runs) available() bool {
	return r != nil && r.db != nil && len(r.key) >= 32 && r.verifier != nil && r.ledger.authority != nil
}

func normalizeIdentity(i RunIdentity) (RunIdentity, error) {
	if i.AuthorizationKind == "" {
		i.AuthorizationKind = "documents"
	}
	if !validScope(i.Scope) || !id(i.UserID) || !id(i.SessionID) || !id(i.AttemptID) || len(i.AuthorizedDocumentIDs) > 256 || i.AuthorizationKind != "documents" && i.AuthorizationKind != "knowledge_base" || i.AuthorizationKind == "documents" && len(i.AuthorizedDocumentIDs) == 0 || i.AuthorizationKind == "knowledge_base" && len(i.AuthorizedDocumentIDs) != 0 {
		return i, ErrInvalid
	}
	i.AuthorizedDocumentIDs = append([]string(nil), i.AuthorizedDocumentIDs...)
	sort.Strings(i.AuthorizedDocumentIDs)
	for n, doc := range i.AuthorizedDocumentIDs {
		if !id(doc) || n > 0 && doc == i.AuthorizedDocumentIDs[n-1] {
			return i, ErrInvalid
		}
	}
	return i, nil
}

func loadRun(ctx context.Context, tx *sql.Tx, s Scope, runID string) (RunSnapshot, string, error) {
	var body []byte
	var capability, state string
	var revision int64
	var v RunSnapshot
	e := tx.QueryRowContext(ctx, "SELECT payload_json,capability_hash,state,revision FROM ext_provenance_runs WHERE tenant_id=? AND knowledge_base_id=? AND run_id=?", args(s, runID)...).Scan(&body, &capability, &state, &revision)
	if e != nil {
		return v, "", e
	}
	if json.Unmarshal(body, &v) != nil || v.Identity.Scope != s || v.RunID != runID || v.State != state || v.Revision != revision {
		return v, "", ErrUnavailable
	}
	return v, capability, nil
}

func (r *Runs) authenticated(ctx context.Context, tx *sql.Tx, h RunHandle, mutate bool) (RunSnapshot, error) {
	v, capHash, e := loadRun(ctx, tx, h.Scope, h.RunID)
	if e != nil || !digest(h.Capability) || !hmac.Equal([]byte(h.Capability), []byte(r.capability(h.Scope, h.RunID))) || !hmac.Equal([]byte(capHash), []byte(payloadHash("capability", h.Capability))) {
		return RunSnapshot{}, ErrEvidence
	}
	if mutate && (!r.now().Before(v.ExpiresAt) || v.State != "open") {
		return RunSnapshot{}, ErrConflict
	}
	return v, nil
}

func writeRun(ctx context.Context, tx *sql.Tx, v RunSnapshot) error {
	body, e := json.Marshal(v)
	if e != nil || len(body) > 128<<10 {
		return ErrInvalid
	}
	_, e = tx.ExecContext(ctx, "UPDATE ext_provenance_runs SET revision=?,state=?,payload_json=? WHERE tenant_id=? AND knowledge_base_id=? AND run_id=?", append([]any{v.Revision, v.State, string(body)}, args(v.Identity.Scope, v.RunID)...)...)
	return e
}

func runReplay(ctx context.Context, tx *sql.Tx, s Scope, op, kind, hash string, result any) (bool, error) {
	var oldKind, oldHash string
	var body []byte
	e := tx.QueryRowContext(ctx, "SELECT kind,payload_hash,result_json FROM ext_provenance_run_receipts WHERE tenant_id=? AND knowledge_base_id=? AND operation_id=?", args(s, op)...).Scan(&oldKind, &oldHash, &body)
	if errors.Is(e, sql.ErrNoRows) {
		return false, nil
	}
	if e != nil {
		return false, e
	}
	if oldKind != kind || oldHash != hash {
		return true, ErrConflict
	}
	if result != nil && json.Unmarshal(body, result) != nil {
		return true, ErrUnavailable
	}
	return true, nil
}

func (r *Runs) runReceipt(ctx context.Context, tx *sql.Tx, v RunSnapshot, op, kind, hash string, result any) error {
	body, e := json.Marshal(result)
	if e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, "INSERT INTO ext_provenance_run_receipts VALUES(?,?,?,?,?,?,?)", args(v.Identity.Scope, op, v.RunID, kind, hash, string(body))...); e != nil {
		return e
	}
	_, e = tx.ExecContext(ctx, "INSERT INTO ext_provenance_run_audit(tenant_id,knowledge_base_id,operation_id,run_id,revision,state,occurred_at) VALUES(?,?,?,?,?,?,?)", args(v.Identity.Scope, op, v.RunID, v.Revision, v.State, r.now().UTC().Format(time.RFC3339Nano))...)
	return e
}

// Begin atomically binds a new random run to an immutable authorization scope.
func (r *Runs) Begin(ctx context.Context, i RunIdentity, op string) (h RunHandle, err error) {
	if !r.available() {
		return h, ErrUnavailable
	}
	i, err = normalizeIdentity(i)
	if err != nil || !id(op) {
		return h, ErrInvalid
	}
	hash := payloadHash("begin", i)
	err = r.db.Write(ctx, func(tx *sql.Tx) error {
		var v RunSnapshot
		if exists, e := runReplay(ctx, tx, i.Scope, op, "begin", hash, &v); e != nil {
			return e
		} else if exists {
			h = RunHandle{i.Scope, v.RunID, r.capability(i.Scope, v.RunID)}
			_, e = r.authenticated(ctx, tx, h, false)
			return e
		}
		runID, e := randomFactID()
		if e != nil {
			return e
		}
		h = RunHandle{i.Scope, runID, r.capability(i.Scope, runID)}
		now := r.now().UTC()
		v = RunSnapshot{Identity: i, RunID: runID, Revision: 1, State: "open", Sources: []SourceRef{}, CreatedAt: now, ExpiresAt: now.Add(24 * time.Hour)}
		body, _ := json.Marshal(v)
		if _, e = tx.ExecContext(ctx, "INSERT INTO ext_provenance_runs VALUES(?,?,?,?,?,?,?)", args(i.Scope, runID, v.Revision, v.State, payloadHash("capability", h.Capability), string(body))...); e != nil {
			return e
		}
		return r.runReceipt(ctx, tx, v, op, "begin", hash, v)
	})
	if err != nil {
		return RunHandle{}, err
	}
	return h, nil
}

// Inspect returns server facts only after validating the exact capability/scope.
func (r *Runs) Inspect(ctx context.Context, h RunHandle) (v RunSnapshot, err error) {
	if !r.available() {
		return v, ErrUnavailable
	}
	err = r.db.View(ctx, func(tx *sql.Tx) error { var e error; v, e = r.authenticated(ctx, tx, h, false); return e })
	return
}

func (r *Runs) mutate(ctx context.Context, h RunHandle, op, kind string, payload, result any, apply func(*sql.Tx, *RunSnapshot) error) error {
	if !r.available() {
		return ErrUnavailable
	}
	if !validScope(h.Scope) || !id(h.RunID) || !id(op) {
		return ErrInvalid
	}
	hash := payloadHash(kind, struct {
		RunID   string
		Payload any
	}{h.RunID, payload})
	return r.db.Write(ctx, func(tx *sql.Tx) error {
		v, e := r.authenticated(ctx, tx, h, false)
		if e != nil {
			return e
		}
		if exists, e := runReplay(ctx, tx, h.Scope, op, kind, hash, result); e != nil || exists {
			return e
		}
		if v.State != "open" || !r.now().Before(v.ExpiresAt) {
			return ErrConflict
		}
		if e = apply(tx, &v); e != nil {
			return e
		}
		v.Revision++
		if e = writeRun(ctx, tx, v); e != nil {
			return e
		}
		return r.runReceipt(ctx, tx, v, op, kind, hash, result)
	})
}

// replayMutation acknowledges a committed fact even after sealing or expiry.
// It is never a current permission check and never repeats a side effect.
func (r *Runs) replayMutation(ctx context.Context, h RunHandle, op, kind string, payload, result any) (exists bool, err error) {
	if !r.available() {
		return false, ErrUnavailable
	}
	hash := payloadHash(kind, struct {
		RunID   string
		Payload any
	}{h.RunID, payload})
	err = r.db.View(ctx, func(tx *sql.Tx) error {
		if _, e := r.authenticated(ctx, tx, h, false); e != nil {
			return e
		}
		var e error
		exists, e = runReplay(ctx, tx, h.Scope, op, kind, hash, result)
		return e
	})
	return
}
