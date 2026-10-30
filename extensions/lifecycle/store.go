package lifecycle

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/outbox"
	"github.com/ybochuan/LanRAG-Next/extensions/store"
)

var (
	// ErrEventConflict rejects reuse of an event ID or source key with changed data.
	ErrEventConflict = errors.New("lifecycle event identity conflicts with stored data")
	// ErrStateConflict rejects a transition based on a stale or invented current state.
	ErrStateConflict = errors.New("lifecycle from_state does not match current state")
)

// Scope identifies one document within its tenant and knowledge base.
type Scope struct {
	TenantID        uint64
	KnowledgeBaseID string
	DocumentID      string
}

// Current is the materialized lifecycle fact, reproducible from the event log.
type Current struct {
	State       State
	LastEventID string
}

// Store persists lifecycle facts and their projection intent in the extension DB.
type Store struct{ db *store.DB }

// NewStore uses the shared WriteExecutor; it does not own or close the database.
func NewStore(db *store.DB) *Store { return &Store{db: db} }

func (scope Scope) validate() error {
	if scope.TenantID == 0 || scope.KnowledgeBaseID == "" || scope.DocumentID == "" {
		return errors.New("complete lifecycle scope is required")
	}
	return nil
}

func (scope Scope) args() []any {
	return []any{strconv.FormatUint(scope.TenantID, 10), scope.KnowledgeBaseID, scope.DocumentID}
}

// Append returns true only after committing event/current/outbox together. A byte-
// equivalent canonical retry is a no-op, even after subsequent transitions. State
// policy belongs to M2: this layer only checks scope, identity and from_state.
func (s *Store) Append(ctx context.Context, event Event) (bool, error) {
	scope := Scope{event.TenantID, event.KnowledgeBaseID, event.DocumentID}
	if err := scope.validate(); err != nil {
		return false, err
	}
	if event.EventID == "" || event.ToState == "" || event.Reason == "" || event.OccurredAt.IsZero() || event.Attempt < 0 || (event.SourceEventID != "" && event.Stage == "") {
		return false, errors.New("incomplete lifecycle event")
	}
	event.OccurredAt = event.OccurredAt.UTC()
	payload, err := json.Marshal(event)
	if err != nil {
		return false, err
	}
	inserted := false
	err = s.db.Write(ctx, func(tx *sql.Tx) error {
		query := "SELECT payload_json FROM ext_lifecycle_events WHERE event_id=?"
		args := []any{event.EventID}
		if event.SourceEventID != "" {
			query += " OR (tenant_id=? AND knowledge_base_id=? AND document_id=? AND source_event_id=? AND stage=? AND attempt=?)"
			args = append(args, scope.args()...)
			args = append(args, event.SourceEventID, event.Stage, event.Attempt)
		}
		rows, err := tx.QueryContext(ctx, query, args...)
		if err != nil {
			return err
		}
		duplicate := false
		for rows.Next() {
			var stored []byte
			if err := rows.Scan(&stored); err != nil {
				rows.Close()
				return err
			}
			if !bytes.Equal(stored, payload) {
				rows.Close()
				return ErrEventConflict
			}
			duplicate = true
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		if duplicate {
			return nil
		}
		var from State
		err = tx.QueryRowContext(ctx, "SELECT state FROM ext_lifecycle_current WHERE tenant_id=? AND knowledge_base_id=? AND document_id=?", scope.args()...).Scan(&from)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if event.FromState != from {
			return ErrStateConflict
		}
		var source any
		if event.SourceEventID != "" {
			source = event.SourceEventID
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO ext_lifecycle_events
			(event_id,tenant_id,knowledge_base_id,document_id,from_state,to_state,reason,occurred_at,source_event_id,stage,attempt,payload_json)
			VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, event.EventID, strconv.FormatUint(event.TenantID, 10), event.KnowledgeBaseID, event.DocumentID,
			event.FromState, event.ToState, event.Reason, event.OccurredAt.Format(time.RFC3339Nano), source, event.Stage, event.Attempt, string(payload))
		if err != nil {
			return err
		}
		args = append(scope.args(), event.ToState, event.EventID)
		_, err = tx.ExecContext(ctx, `INSERT INTO ext_lifecycle_current(tenant_id,knowledge_base_id,document_id,state,last_event_id)
			VALUES(?,?,?,?,?) ON CONFLICT(tenant_id,knowledge_base_id,document_id) DO UPDATE SET state=excluded.state,last_event_id=excluded.last_event_id`, args...)
		if err != nil {
			return err
		}
		projection, err := json.Marshal(struct {
			DocumentID string `json:"document_id"`
			State      State  `json:"state"`
		}{event.DocumentID, event.ToState})
		if err != nil {
			return err
		}
		if err := outbox.EnqueueTx(ctx, tx, outbox.Item{ID: "lifecycle:" + event.EventID, TenantID: event.TenantID, KnowledgeBaseID: event.KnowledgeBaseID,
			ProjectionType: "tag_projection", AggregateType: "document", AggregateID: event.DocumentID, CausationType: "lifecycle_event", CausationID: event.EventID,
			Payload: projection, CreatedAt: time.Now().UTC()}); err != nil {
			return err
		}
		inserted = true
		return nil
	})
	if err != nil {
		return false, fmt.Errorf("append lifecycle event: %w", err)
	}
	return inserted, nil
}

// Current returns sql.ErrNoRows when the scoped document has no lifecycle fact.
func (s *Store) Current(ctx context.Context, scope Scope) (Current, error) {
	var current Current
	if err := scope.validate(); err != nil {
		return current, err
	}
	err := s.db.View(ctx, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, "SELECT state,last_event_id FROM ext_lifecycle_current WHERE tenant_id=? AND knowledge_base_id=? AND document_id=?", scope.args()...).Scan(&current.State, &current.LastEventID)
	})
	return current, err
}

// Events returns committed events in append sequence, rather than producer-clock order.
func (s *Store) Events(ctx context.Context, scope Scope) ([]Event, error) {
	if err := scope.validate(); err != nil {
		return nil, err
	}
	events := make([]Event, 0)
	err := s.db.View(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, "SELECT payload_json FROM ext_lifecycle_events WHERE tenant_id=? AND knowledge_base_id=? AND document_id=? ORDER BY sequence", scope.args()...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var payload []byte
			if err := rows.Scan(&payload); err != nil {
				return err
			}
			var event Event
			if err := json.Unmarshal(payload, &event); err != nil {
				return err
			}
			events = append(events, event)
		}
		return rows.Err()
	})
	return events, err
}
