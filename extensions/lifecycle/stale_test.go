package lifecycle

import (
	"testing"
	"time"
)

func TestIndependentStaleAndReview(t *testing.T) {
	now := time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC)
	past := now.Add(-time.Second)
	future := now.Add(time.Hour)
	doc := AssessDocument(now, Annotations{EffectiveUntil: &future, ReviewDue: &past})
	guidance := AssessGuidance(now, GuidanceFreshness{EffectiveUntil: &past})
	if doc.Stale || !doc.ReviewOverdue || !guidance.Stale {
		t.Fatalf("doc=%+v guidance=%+v", doc, guidance)
	}
	parent := SupplementEvidence{Scope: Scope{7, "kb-a", "parent"}, LastEventID: "parent-event", Annotations: Annotations{EffectiveUntil: &past}}
	result, err := AssessSupplement(now, Scope{7, "kb-a", "child"}, Annotations{}, parent)
	if err != nil || !result.Stale {
		t.Fatalf("%+v %v", result, err)
	}
	parent.Scope.TenantID = 8
	if _, err = AssessSupplement(now, Scope{7, "kb-a", "child"}, Annotations{}, parent); err == nil {
		t.Fatal("cross tenant parent accepted")
	}
	if AssessDocument(now, Annotations{}).Stale {
		t.Fatal("unset expiry became stale")
	}
}
