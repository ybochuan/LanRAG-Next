package lifecycle

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/store"
)

// ErrConfigurationConflict rejects stale annotation or resolved rule revisions.
var ErrConfigurationConflict = errors.New("lifecycle configuration revision conflicts")

// PolicyScope permits an empty KB only for a tenant-wide default rule row.
type PolicyScope struct {
	TenantID        uint64 `json:"tenant_id"`
	KnowledgeBaseID string `json:"knowledge_base_id"`
}

// Rules supplies missing dates on initialization only. Zero days disables the default.
// A KB row completely replaces its tenant row; calendar days are measured in UTC.
type Rules struct {
	EffectiveDelayDays int `json:"effective_delay_days"`
	ValidDays          int `json:"valid_days"`
	ReviewDays         int `json:"review_days"`
}

// ResolvedRules includes an opaque token fencing both the selected row and body.
type ResolvedRules struct {
	Rules    Rules       `json:"rules"`
	Source   PolicyScope `json:"source"`
	Revision int64       `json:"revision"`
	Token    string      `json:"token"`
}

// Annotations is the data-driven validity and classification record. Nil dates
// mean unspecified/permanent; ReviewDue is advisory and never a state transition.
type Annotations struct {
	Revision       int64             `json:"revision"`
	EffectiveFrom  *time.Time        `json:"effective_from,omitempty"`
	EffectiveUntil *time.Time        `json:"effective_until,omitempty"`
	ReviewDue      *time.Time        `json:"review_due,omitempty"`
	Labels         map[string]string `json:"labels"`
}

// Configuration reads and writes extension-owned configuration through shared transactions.
type Configuration struct{ db *store.DB }

// NewConfiguration uses the shared extension DB and does not own its lifetime.
func NewConfiguration(db *store.DB) *Configuration { return &Configuration{db: db} }

// PutRules creates at expected=0 or replaces exactly the expected revision.
func (c *Configuration) PutRules(ctx context.Context, s PolicyScope, r Rules, expected int64) error {
	if s.TenantID == 0 || expected < 0 {
		return errors.New("valid policy scope and revision required")
	}
	for _, days := range []int{r.EffectiveDelayDays, r.ValidDays, r.ReviewDays} {
		if days < 0 || days > 36500 {
			return errors.New("default days must be in 0..36500")
		}
	}
	body, err := json.Marshal(r)
	if err != nil {
		return err
	}
	return c.db.Write(ctx, func(tx *sql.Tx) error {
		var revision int64
		err := tx.QueryRowContext(ctx, "SELECT revision FROM ext_lifecycle_rules WHERE tenant_id=? AND knowledge_base_id=?", strconv.FormatUint(s.TenantID, 10), s.KnowledgeBaseID).Scan(&revision)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if revision != expected {
			return ErrConfigurationConflict
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO ext_lifecycle_rules(tenant_id,knowledge_base_id,revision,rules_json) VALUES(?,?,?,?) ON CONFLICT(tenant_id,knowledge_base_id) DO UPDATE SET revision=excluded.revision,rules_json=excluded.rules_json`, strconv.FormatUint(s.TenantID, 10), s.KnowledgeBaseID, expected+1, string(body))
		return err
	})
}

// Resolve returns KB override, otherwise tenant default, otherwise permanent validity.
func (c *Configuration) Resolve(ctx context.Context, s PolicyScope) (r ResolvedRules, err error) {
	if s.TenantID == 0 || s.KnowledgeBaseID == "" {
		return r, errors.New("explicit tenant and KB required")
	}
	err = c.db.View(ctx, func(tx *sql.Tx) error { r, err = resolveRulesTx(ctx, tx, s); return err })
	return
}

// ResolveForEdit reads the effective body and the KB-local CAS revision in one snapshot.
// Inherited tenant rules have local revision zero until a KB override is created.
func (c *Configuration) ResolveForEdit(ctx context.Context, s PolicyScope) (r ResolvedRules, local int64, err error) {
	if s.TenantID == 0 || s.KnowledgeBaseID == "" {
		return r, 0, errors.New("explicit tenant and KB required")
	}
	err = c.db.View(ctx, func(tx *sql.Tx) error {
		var e error
		r, e = resolveRulesTx(ctx, tx, s)
		if e != nil {
			return e
		}
		e = tx.QueryRowContext(ctx, "SELECT revision FROM ext_lifecycle_rules WHERE tenant_id=? AND knowledge_base_id=?", strconv.FormatUint(s.TenantID, 10), s.KnowledgeBaseID).Scan(&local)
		if errors.Is(e, sql.ErrNoRows) {
			return nil
		}
		return e
	})
	return
}
func resolveRulesTx(ctx context.Context, tx *sql.Tx, s PolicyScope) (ResolvedRules, error) {
	r := ResolvedRules{Source: s}
	var body []byte
	err := tx.QueryRowContext(ctx, `SELECT knowledge_base_id,revision,rules_json FROM ext_lifecycle_rules WHERE tenant_id=? AND knowledge_base_id IN (?, '') ORDER BY CASE WHEN knowledge_base_id=? THEN 0 ELSE 1 END LIMIT 1`, strconv.FormatUint(s.TenantID, 10), s.KnowledgeBaseID, s.KnowledgeBaseID).Scan(&r.Source.KnowledgeBaseID, &r.Revision, &body)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return r, err
	}
	if err == nil {
		if err = json.Unmarshal(body, &r.Rules); err != nil {
			return r, err
		}
	}
	canonical, err := json.Marshal(r)
	if err != nil {
		return r, err
	}
	r.Token = fmt.Sprintf("%x", sha256.Sum256(canonical))
	return r, nil
}
func normalizeAnnotations(a Annotations) (Annotations, error) {
	for _, p := range []**time.Time{&a.EffectiveFrom, &a.EffectiveUntil, &a.ReviewDue} {
		if *p != nil {
			v := (**p).UTC()
			if v.IsZero() || v.Year() < 1 || v.Year() > 9999 {
				return a, errors.New("valid annotation UTC dates required")
			}
			*p = &v
		}
	}
	if a.EffectiveFrom != nil && a.EffectiveUntil != nil && !a.EffectiveUntil.After(*a.EffectiveFrom) {
		return a, errors.New("effective_until must follow effective_from")
	}
	if len(a.Labels) > 32 {
		return a, errors.New("at most 32 labels allowed")
	}
	labels := make(map[string]string, len(a.Labels))
	for key, value := range a.Labels {
		if key == "" || len(key) > 128 || value == "" || len(value) > 1024 {
			return a, errors.New("invalid annotation label")
		}
		labels[key] = value
	}
	a.Labels = labels
	return a, nil
}

// PutAnnotations replaces the complete scoped annotation record using revision CAS.
// Rules do not silently repopulate explicitly cleared dates on later edits.
func (c *Configuration) PutAnnotations(ctx context.Context, s Scope, a Annotations, expected int64) error {
	return c.putAnnotations(ctx, s, a, expected, nil)
}

// PutManagementAnnotations refuses date edits requiring transitions absent from the frozen matrix.
// The state check shares the annotation CAS write transaction, including concurrent timer scans.
func (c *Configuration) PutManagementAnnotations(ctx context.Context, s Scope, a Annotations, expected int64, now time.Time) error {
	return c.putAnnotations(ctx, s, a, expected, func(tx *sql.Tx) error {
		var state State
		if err := tx.QueryRowContext(ctx, "SELECT state FROM ext_lifecycle_current WHERE tenant_id=? AND knowledge_base_id=? AND document_id=?", s.args()...).Scan(&state); err != nil {
			return err
		}
		if state == Active && a.EffectiveFrom != nil && a.EffectiveFrom.After(now) || state == Scheduled && a.EffectiveFrom == nil {
			return errors.New("unsupported effective_from change for current state")
		}
		return nil
	})
}

func (c *Configuration) putAnnotations(ctx context.Context, s Scope, a Annotations, expected int64, guard func(*sql.Tx) error) error {
	if err := s.validate(); err != nil {
		return err
	}
	if expected < 0 {
		return ErrConfigurationConflict
	}
	var err error
	a, err = normalizeAnnotations(a)
	if err != nil {
		return err
	}
	return c.db.Write(ctx, func(tx *sql.Tx) error {
		old, err := annotationsTx(ctx, tx, s)
		if err != nil {
			return err
		}
		if old.Revision != expected {
			return ErrConfigurationConflict
		}
		if guard != nil {
			if err := guard(tx); err != nil {
				return err
			}
		}
		a.Revision = expected + 1
		return saveAnnotationsTx(ctx, tx, s, a)
	})
}

// Annotations returns sql.ErrNoRows when no annotation has been initialized.
func (c *Configuration) Annotations(ctx context.Context, s Scope) (a Annotations, err error) {
	if err = s.validate(); err != nil {
		return
	}
	err = c.db.View(ctx, func(tx *sql.Tx) error {
		a, err = annotationsTx(ctx, tx, s)
		if err == nil && a.Revision == 0 {
			return sql.ErrNoRows
		}
		return err
	})
	return
}
func annotationsTx(ctx context.Context, tx *sql.Tx, s Scope) (a Annotations, err error) {
	var from, until, review sql.NullString
	var labels []byte
	err = tx.QueryRowContext(ctx, `SELECT revision,effective_from,effective_until,review_due,labels_json FROM ext_document_annotations WHERE tenant_id=? AND knowledge_base_id=? AND document_id=?`, s.args()...).Scan(&a.Revision, &from, &until, &review, &labels)
	if errors.Is(err, sql.ErrNoRows) {
		return a, nil
	}
	if err != nil {
		return
	}
	for _, entry := range []struct {
		value  sql.NullString
		target **time.Time
	}{{from, &a.EffectiveFrom}, {until, &a.EffectiveUntil}, {review, &a.ReviewDue}} {
		if entry.value.Valid {
			var v time.Time
			v, err = time.Parse(time.RFC3339Nano, entry.value.String)
			if err != nil {
				return
			}
			v = v.UTC()
			*entry.target = &v
		}
	}
	err = json.Unmarshal(labels, &a.Labels)
	return
}
func dateValue(v *time.Time) any {
	if v == nil {
		return nil
	}
	return v.UTC().Format(time.RFC3339Nano)
}
func saveAnnotationsTx(ctx context.Context, tx *sql.Tx, s Scope, a Annotations) error {
	labels, err := json.Marshal(a.Labels)
	if err != nil {
		return err
	}
	args := append(s.args(), a.Revision, dateValue(a.EffectiveFrom), dateValue(a.EffectiveUntil), dateValue(a.ReviewDue), string(labels))
	_, err = tx.ExecContext(ctx, `INSERT INTO ext_document_annotations(tenant_id,knowledge_base_id,document_id,revision,effective_from,effective_until,review_due,labels_json) VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(tenant_id,knowledge_base_id,document_id) DO UPDATE SET revision=excluded.revision,effective_from=excluded.effective_from,effective_until=excluded.effective_until,review_due=excluded.review_due,labels_json=excluded.labels_json`, args...)
	if err != nil {
		return err
	}
	// Preserve an observed deadline change even if it changes back before a scan.
	resolveArgs := append([]any{time.Now().UTC().Format(time.RFC3339Nano)}, s.args()...)
	resolveArgs = append(resolveArgs, dateValue(a.ReviewDue))
	_, err = tx.ExecContext(ctx, `UPDATE ext_review_alerts SET status='resolved',resolved_at=? WHERE tenant_id=? AND knowledge_base_id=? AND document_id=? AND status='open' AND review_due IS NOT ?`, resolveArgs...)
	return err
}
