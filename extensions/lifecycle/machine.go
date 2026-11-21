package lifecycle

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/dependencies"
	"github.com/ybochuan/LanRAG-Next/extensions/store"
)

// Core states implement the user-approved M2 default matrix.
const (
	Pending   State = "pending"
	Scheduled State = "scheduled"
	Active    State = "active"
	Stale     State = "stale"
)

// Trigger identifies the trusted extension action, not a raw native parse status.
type Trigger string

const (
	Initialize            Trigger = "initialize"
	ParseCompleted        Trigger = "parse_completed"
	Timer                 Trigger = "timer"
	DependencyInvalidated Trigger = "dependency_invalidated"
)

// ErrTransitionDenied rejects an unsupported edge, trigger or validity window.
var ErrTransitionDenied = errors.New("lifecycle transition denied")

// Command binds the decision to exactly the facts inspected by its caller.
type Command struct {
	Event                      Event      `json:"event"`
	Trigger                    Trigger    `json:"trigger"`
	ExpectedLastEventID        string     `json:"expected_last_event_id"`
	ExpectedAnnotationRevision int64      `json:"expected_annotation_revision"`
	ExpectedPolicyToken        string     `json:"expected_policy_token"`
	ParseObservationAt         *time.Time `json:"parse_observation_at,omitempty"`
}

// Snapshot is a consistent scoped read. ReviewOverdue is advisory only.
type Snapshot struct {
	Current Current `json:"current"`
	// EffectiveState is the state shown to operators. An attachment inherits a
	// non-active parent's lifecycle state without copying it into the child's own
	// event stream; when the parent is active the child keeps its own state.
	// Current remains the child's persisted lifecycle fact.
	EffectiveState             State                   `json:"effective_state,omitempty"`
	Annotations                Annotations             `json:"annotations"`
	Policy                     ResolvedRules           `json:"policy"`
	ReviewOverdue              bool                    `json:"review_overdue"`
	Dependencies               dependencies.Assessment `json:"dependencies"`
	AttachmentParentDocumentID string                  `json:"attachment_parent_document_id,omitempty"`
}

// Machine applies extension-only decisions. It does not own its DB or schedule work.
type Machine struct {
	db  *store.DB
	now func() time.Time
}

// NewMachine accepts an injectable clock; nil uses the system clock.
func NewMachine(db *store.DB, now func() time.Time) *Machine {
	if now == nil {
		now = time.Now
	}
	return &Machine{db, now}
}

// attachmentAnnotationsTx overlays an attachment's effective window with its
// active parent's annotations and reads the parent's lifecycle state. The child
// keeps its own lifecycle facts and review metadata; only the validity dates and
// the display state are inherited so parent edits take effect without copying
// rows into every attachment.
func attachmentAnnotationsTx(ctx context.Context, tx *sql.Tx, s Scope) (Annotations, string, State, error) {
	annotations, err := annotationsTx(ctx, tx, s)
	if err != nil {
		return annotations, "", "", err
	}
	var parent string
	err = tx.QueryRowContext(ctx, `SELECT parent_document_id FROM ext_attachment_links WHERE tenant_id=? AND knowledge_base_id=? AND attachment_id=? AND removed_at IS NULL AND retired_at IS NULL ORDER BY created_at DESC LIMIT 1`, s.args()...).Scan(&parent)
	if errors.Is(err, sql.ErrNoRows) {
		return annotations, "", "", nil
	}
	if err != nil {
		return annotations, "", "", err
	}
	parentAnnotations, err := annotationsTx(ctx, tx, Scope{TenantID: s.TenantID, KnowledgeBaseID: s.KnowledgeBaseID, DocumentID: parent})
	if err != nil {
		return annotations, "", "", err
	}
	parentState, _, stateErr := store.ReadLifecycleCurrentTx(ctx, tx, s.TenantID, s.KnowledgeBaseID, parent)
	if stateErr != nil && !errors.Is(stateErr, sql.ErrNoRows) {
		return annotations, "", "", stateErr
	}
	annotations.EffectiveFrom = parentAnnotations.EffectiveFrom
	annotations.EffectiveUntil = parentAnnotations.EffectiveUntil
	return annotations, parent, State(parentState), nil
}

func snapshotTx(ctx context.Context, tx *sql.Tx, s Scope) (snap Snapshot, err error) {
	state, event, readErr := store.ReadLifecycleCurrentTx(ctx, tx, s.TenantID, s.KnowledgeBaseID, s.DocumentID)
	snap.Current.State, snap.Current.LastEventID, err = State(state), event, readErr
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return
	}
	var parentState State
	snap.Annotations, snap.AttachmentParentDocumentID, parentState, err = attachmentAnnotationsTx(ctx, tx, s)
	if err != nil {
		return
	}
	snap.EffectiveState = snap.Current.State
	if parentState != "" && parentState != Active {
		snap.EffectiveState = parentState
	}
	snap.Policy, err = resolveRulesTx(ctx, tx, PolicyScope{s.TenantID, s.KnowledgeBaseID})
	if err == nil {
		snap.Dependencies, err = dependencies.LoadTx(ctx, tx, dependencies.Scope{TenantID: s.TenantID, KnowledgeBaseID: s.KnowledgeBaseID, DocumentID: s.DocumentID})
	}
	return
}

// AnnotationsForDocuments reads the effective annotations for a bounded page in
// one transaction. Attachment children inherit their active parent's effective
// window without writing copied dates into the child's own annotation row.
func (m *Machine) AnnotationsForDocuments(ctx context.Context, s Scope, documentIDs []string) (map[string]Annotations, error) {
	if s.TenantID == 0 || s.KnowledgeBaseID == "" {
		return nil, errors.New("complete lifecycle scope is required")
	}
	result := make(map[string]Annotations, len(documentIDs))
	if len(documentIDs) == 0 {
		return result, nil
	}
	body, err := json.Marshal(documentIDs)
	if err != nil {
		return nil, err
	}
	err = m.db.View(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT document_id,revision,effective_from,effective_until,review_due,labels_json
FROM ext_document_annotations
WHERE tenant_id=? AND knowledge_base_id=? AND document_id IN (SELECT value FROM json_each(?))`, s.args()[0], s.args()[1], string(body))
		if err != nil {
			return err
		}
		annotations, err := scanAnnotationRows(rows)
		rows.Close()
		if err != nil {
			return err
		}
		for id, value := range annotations {
			result[id] = value
		}
		parentRows, err := tx.QueryContext(ctx, `SELECT l.attachment_id,l.parent_document_id
FROM ext_attachment_links l
WHERE l.tenant_id=? AND l.knowledge_base_id=? AND l.attachment_id IN (SELECT value FROM json_each(?))
AND l.removed_at IS NULL AND l.retired_at IS NULL
AND l.created_at=(SELECT MAX(l2.created_at) FROM ext_attachment_links l2
 WHERE l2.tenant_id=l.tenant_id AND l2.knowledge_base_id=l.knowledge_base_id AND l2.attachment_id=l.attachment_id
 AND l2.removed_at IS NULL AND l2.retired_at IS NULL)`, s.args()[0], s.args()[1], string(body))
		if err != nil {
			return err
		}
		parentIDs := make(map[string]string)
		for parentRows.Next() {
			var child, parent string
			if err := parentRows.Scan(&child, &parent); err != nil {
				parentRows.Close()
				return err
			}
			parentIDs[child] = parent
		}
		if err := parentRows.Err(); err != nil {
			parentRows.Close()
			return err
		}
		parentRows.Close()
		if len(parentIDs) == 0 {
			return nil
		}
		parentList := make([]string, 0, len(parentIDs))
		seen := make(map[string]bool)
		for _, parent := range parentIDs {
			if !seen[parent] {
				seen[parent] = true
				parentList = append(parentList, parent)
			}
		}
		parentBody, err := json.Marshal(parentList)
		if err != nil {
			return err
		}
		rows, err = tx.QueryContext(ctx, `SELECT document_id,revision,effective_from,effective_until,review_due,labels_json
FROM ext_document_annotations
WHERE tenant_id=? AND knowledge_base_id=? AND document_id IN (SELECT value FROM json_each(?))`, s.args()[0], s.args()[1], string(parentBody))
		if err != nil {
			return err
		}
		parentAnnotations, err := scanAnnotationRows(rows)
		rows.Close()
		if err != nil {
			return err
		}
		for child, parent := range parentIDs {
			value := result[child]
			// The parent's validity window is authoritative, including an
			// absent parent annotation row (which means no effective window).
			value.EffectiveFrom = nil
			value.EffectiveUntil = nil
			if parentValue, ok := parentAnnotations[parent]; ok {
				value.EffectiveFrom = parentValue.EffectiveFrom
				value.EffectiveUntil = parentValue.EffectiveUntil
			}
			result[child] = value
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// Snapshot returns empty current/annotations for a new document, never another scope.
func (m *Machine) Snapshot(ctx context.Context, s Scope) (snap Snapshot, err error) {
	if err = s.validate(); err != nil {
		return
	}
	err = m.db.View(ctx, func(tx *sql.Tx) error { snap, err = snapshotTx(ctx, tx, s); return err })
	if err == nil {
		snap.ReviewOverdue = overdue(m.now().UTC(), snap.Annotations.ReviewDue) || snap.Dependencies.ReviewOverdue(m.now().UTC())
	}
	return
}

// Apply commits event/current/Outbox and the complete decision receipt together.
// Exact command retries remain no-ops after time, annotations or rules change.
func (m *Machine) Apply(ctx context.Context, c Command) (bool, error) {
	if c.ExpectedAnnotationRevision < 0 || c.ExpectedPolicyToken == "" {
		return false, ErrConfigurationConflict
	}
	c.Event.OccurredAt = c.Event.OccurredAt.UTC()
	if c.ParseObservationAt != nil {
		v := c.ParseObservationAt.UTC()
		c.ParseObservationAt = &v
	}
	payload, err := json.Marshal(c)
	if err != nil {
		return false, err
	}
	s := Scope{c.Event.TenantID, c.Event.KnowledgeBaseID, c.Event.DocumentID}
	return NewStore(m.db).appendChecked(ctx, c.Event, func(tx *sql.Tx, duplicate bool) error {
		if duplicate {
			var stored []byte
			err := tx.QueryRowContext(ctx, "SELECT command_json FROM ext_lifecycle_decisions WHERE event_id=?", c.Event.EventID).Scan(&stored)
			if errors.Is(err, sql.ErrNoRows) {
				return ErrEventConflict
			}
			if err != nil {
				return err
			}
			if !bytes.Equal(stored, payload) {
				return ErrEventConflict
			}
			return nil
		}
		snap, err := snapshotTx(ctx, tx, s)
		if err != nil {
			return err
		}
		if snap.Current.LastEventID != c.ExpectedLastEventID {
			return ErrStateConflict
		}
		if snap.Annotations.Revision != c.ExpectedAnnotationRevision || snap.Policy.Token != c.ExpectedPolicyToken {
			return ErrConfigurationConflict
		}
		now := m.now().UTC()
		if now.IsZero() {
			return errors.New("valid decision clock required")
		}
		a := snap.Annotations
		scheduledActivation := c.Trigger == Timer && c.Event.FromState == Scheduled && c.Event.ToState == Active
		if c.ParseObservationAt != nil || scheduledActivation {
			if c.ParseObservationAt != nil && c.Trigger != ParseCompleted {
				return ErrTransitionDenied
			}
			var status, updated string
			err := tx.QueryRowContext(ctx, `SELECT o.parse_status,o.source_updated_at FROM ext_ingest_current i JOIN ext_ingest_observations o ON o.observation_id=i.last_observation_id WHERE i.tenant_id=? AND i.knowledge_base_id=? AND i.document_id=?`, s.args()...).Scan(&status, &updated)
			if errors.Is(err, sql.ErrNoRows) {
				if scheduledActivation {
					return ErrTransitionDenied
				}
				return ErrStateConflict
			}
			if err != nil {
				return err
			}
			at, err := time.Parse(time.RFC3339Nano, updated)
			if err != nil {
				return err
			}
			if status != "completed" && scheduledActivation {
				return ErrTransitionDenied
			}
			if status != "completed" || (c.ParseObservationAt != nil && !at.Equal(*c.ParseObservationAt)) {
				return ErrStateConflict
			}
		}
		if c.Trigger == Initialize && c.Event.FromState == "" && c.Event.ToState == Pending {
			if a.EffectiveFrom == nil && snap.Policy.Rules.EffectiveDelayDays > 0 {
				v := now.AddDate(0, 0, snap.Policy.Rules.EffectiveDelayDays)
				a.EffectiveFrom = &v
			}
			if a.EffectiveUntil == nil && snap.Policy.Rules.ValidDays > 0 {
				base := now
				if a.EffectiveFrom != nil {
					base = *a.EffectiveFrom
				}
				v := base.AddDate(0, 0, snap.Policy.Rules.ValidDays)
				a.EffectiveUntil = &v
			}
			if a.ReviewDue == nil && snap.Policy.Rules.ReviewDays > 0 {
				v := now.AddDate(0, 0, snap.Policy.Rules.ReviewDays)
				a.ReviewDue = &v
			}
			a, err = normalizeAnnotations(a)
			if err != nil {
				return err
			}
			a.Revision++
			if err = saveAnnotationsTx(ctx, tx, s, a); err != nil {
				return err
			}
		} else if c.Trigger == DependencyInvalidated {
			if c.Event.ToState != Stale || (c.Event.FromState != Pending && c.Event.FromState != Scheduled && c.Event.FromState != Active) || !snap.Dependencies.Configured || !snap.Dependencies.Invalid(now) {
				return ErrTransitionDenied
			}
		} else if !allowed(c, a, now) {
			return ErrTransitionDenied
		}
		annotations, err := json.Marshal(a)
		if err != nil {
			return err
		}
		policy, err := json.Marshal(snap.Policy)
		if err != nil {
			return err
		}
		args := append([]any{c.Event.EventID}, s.args()...)
		args = append(args, string(payload), string(annotations), string(policy), now.Format(time.RFC3339Nano))
		_, err = tx.ExecContext(ctx, `INSERT INTO ext_lifecycle_decisions(event_id,tenant_id,knowledge_base_id,document_id,command_json,annotations_json,policy_json,evaluated_at) VALUES(?,?,?,?,?,?,?,?)`, args...)
		return err
	})
}
func scanAnnotationRows(rows *sql.Rows) (map[string]Annotations, error) {
	result := make(map[string]Annotations)
	for rows.Next() {
		var id string
		var annotation Annotations
		var from, until, review sql.NullString
		var labels []byte
		if err := rows.Scan(&id, &annotation.Revision, &from, &until, &review, &labels); err != nil {
			return nil, err
		}
		for _, entry := range []struct {
			value  sql.NullString
			target **time.Time
		}{{from, &annotation.EffectiveFrom}, {until, &annotation.EffectiveUntil}, {review, &annotation.ReviewDue}} {
			if entry.value.Valid {
				value, err := time.Parse(time.RFC3339Nano, entry.value.String)
				if err != nil {
					return nil, err
				}
				value = value.UTC()
				*entry.target = &value
			}
		}
		if err := json.Unmarshal(labels, &annotation.Labels); err != nil {
			return nil, err
		}
		result[id] = annotation
	}
	return result, rows.Err()
}

func allowed(c Command, a Annotations, now time.Time) bool {
	from, to := c.Event.FromState, c.Event.ToState
	expired := overdue(now, a.EffectiveUntil)
	future := a.EffectiveFrom != nil && now.Before(*a.EffectiveFrom)
	if to == Stale && expired && (from == Pending || from == Scheduled || from == Active) && (c.Trigger == Timer || (c.Trigger == ParseCompleted && from == Pending)) {
		return true
	}
	if c.Trigger == ParseCompleted && from == Pending {
		return !expired && ((to == Scheduled && future) || (to == Active && !future))
	}
	if c.Trigger == Timer && from == Scheduled && to == Active {
		return a.EffectiveFrom != nil && !future && !expired
	}
	return c.Trigger == Timer && from == Active && to == Stale && expired
}
