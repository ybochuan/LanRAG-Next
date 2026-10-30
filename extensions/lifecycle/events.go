// Package lifecycle defines Extension Host lifecycle data without core dependencies.
package lifecycle

import "time"

// State is an extension lifecycle state, independent of WeKnora parse statuses.
// The transition rules belong to the lifecycle service, not the transport DTO.
type State string

// Event describes a document state transition. It does not persist or deliver it.
type Event struct {
	EventID         string    `json:"event_id"`
	DocumentID      string    `json:"document_id"`
	TenantID        uint64    `json:"tenant_id"`
	KnowledgeBaseID string    `json:"knowledge_base_id"`
	FromState       State     `json:"from_state"`
	ToState         State     `json:"to_state"`
	Reason          string    `json:"reason"`
	OccurredAt      time.Time `json:"occurred_at"` // Producers supply a UTC timestamp.
	RequestID       string    `json:"request_id,omitempty"`

	// Source identity is retained for future durable deduplication/reconciliation.
	// It is distinct from EventID, which identifies the extension lifecycle event.
	SourceEventID string `json:"source_event_id,omitempty"`
	Stage         string `json:"stage,omitempty"`
	Attempt       int    `json:"attempt"` // Preserve the producer's attempt number.
}
