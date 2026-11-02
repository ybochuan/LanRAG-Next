package lifecycle

import (
	"errors"
	"time"
)

// Freshness separates expiration from the advisory review deadline.
type Freshness struct {
	Stale         bool `json:"stale"`
	ReviewOverdue bool `json:"review_overdue"`
}

// GuidanceFreshness concerns guidance alone, never the document fact index.
type GuidanceFreshness struct {
	EffectiveUntil *time.Time `json:"effective_until,omitempty"`
	ReviewDue      *time.Time `json:"review_due,omitempty"`
	SourceChanged  bool       `json:"source_changed"`
}

func overdue(now time.Time, deadline *time.Time) bool { return deadline != nil && now.After(*deadline) }

// AssessDocument leaves lifecycle transitions to Machine; nil expiry is permanent.
func AssessDocument(now time.Time, a Annotations) Freshness {
	return Freshness{overdue(now, a.EffectiveUntil), overdue(now, a.ReviewDue)}
}

// AssessGuidance never changes document lifecycle or truth.
func AssessGuidance(now time.Time, g GuidanceFreshness) Freshness {
	return Freshness{g.SourceChanged || overdue(now, g.EffectiveUntil), overdue(now, g.ReviewDue)}
}

// SupplementEvidence is explicit parent evidence, not an asserted M3 lineage edge.
type SupplementEvidence struct {
	Scope       Scope       `json:"scope"`
	LastEventID string      `json:"last_event_id"`
	Annotations Annotations `json:"annotations"`
	Invalidated bool        `json:"invalidated"`
}

// AssessSupplement propagates a proven parent's expiry/invalidation as an assessment.
// Resolving authoritative parent evidence and automatic propagation belong to M3.
func AssessSupplement(now time.Time, child Scope, a Annotations, parent SupplementEvidence) (Freshness, error) {
	if err := child.validate(); err != nil {
		return Freshness{}, err
	}
	if err := parent.Scope.validate(); err != nil {
		return Freshness{}, err
	}
	if child.TenantID != parent.Scope.TenantID || child.KnowledgeBaseID != parent.Scope.KnowledgeBaseID || child.DocumentID == parent.Scope.DocumentID || parent.LastEventID == "" {
		return Freshness{}, errors.New("explicit distinct same-KB parent evidence required")
	}
	own := AssessDocument(now, a)
	p := AssessDocument(now, parent.Annotations)
	own.Stale = own.Stale || p.Stale || parent.Invalidated
	own.ReviewOverdue = own.ReviewOverdue || p.ReviewOverdue
	return own, nil
}
