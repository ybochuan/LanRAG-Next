package review

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/outbox"
)

func TestAcknowledgeMarksAnOpenAlertRead(t *testing.T) {
	db, s, now, scope := fixture(t)
	ctx := context.Background()
	past := now.Add(-time.Hour)
	annotate(t, db, scope, &past, 0)
	if err := s.Check(ctx, scope); err != nil {
		t.Fatal(err)
	}
	alerts := list(t, s, scope)
	if len(alerts) != 1 {
		t.Fatalf("alerts = %d, want 1", len(alerts))
	}
	if !alerts[0].Unread || alerts[0].AcknowledgedAt != nil {
		t.Fatalf("fresh alert must be unread: %+v", alerts[0])
	}
	if err := s.Acknowledge(ctx, outbox.Scope{TenantID: scope.TenantID, KnowledgeBaseID: scope.KnowledgeBaseID}, alerts[0].AlertID); err != nil {
		t.Fatal(err)
	}
	after := list(t, s, scope)
	if len(after) != 1 || after[0].Unread || after[0].AcknowledgedAt == nil {
		t.Fatalf("alert must be read after acknowledge: %+v", after)
	}
	if !after[0].AcknowledgedAt.Equal(*now) {
		t.Fatalf("acknowledged_at = %v, want %v", after[0].AcknowledgedAt, now)
	}
}

func TestAcknowledgeIsIdempotentAndKeepsTheFirstReadTime(t *testing.T) {
	db, s, now, scope := fixture(t)
	ctx := context.Background()
	past := now.Add(-time.Hour)
	annotate(t, db, scope, &past, 0)
	if err := s.Check(ctx, scope); err != nil {
		t.Fatal(err)
	}
	alerts := list(t, s, scope)
	if len(alerts) != 1 {
		t.Fatalf("alerts = %d, want 1", len(alerts))
	}
	if err := s.Acknowledge(ctx, outbox.Scope{TenantID: scope.TenantID, KnowledgeBaseID: scope.KnowledgeBaseID}, alerts[0].AlertID); err != nil {
		t.Fatal(err)
	}
	// A clock that moves forward must not rewrite the first read time.
	*now = now.Add(2 * time.Hour)
	if err := s.Acknowledge(ctx, outbox.Scope{TenantID: scope.TenantID, KnowledgeBaseID: scope.KnowledgeBaseID}, alerts[0].AlertID); err != nil {
		t.Fatalf("second acknowledge must be a no-op, got %v", err)
	}
	after := list(t, s, scope)
	if len(after) != 1 || after[0].AcknowledgedAt == nil {
		t.Fatalf("alert must stay read: %+v", after)
	}
	if after[0].AcknowledgedAt.Equal(*now) {
		t.Fatalf("acknowledged_at was rewritten to the later clock")
	}
}

func TestAcknowledgeRejectsAnUnknownAlert(t *testing.T) {
	_, s, _, scope := fixture(t)
	err := s.Acknowledge(context.Background(), outbox.Scope{TenantID: scope.TenantID, KnowledgeBaseID: scope.KnowledgeBaseID}, "missing")
	if !errors.Is(err, ErrAlertNotFound) {
		t.Fatalf("err = %v, want ErrAlertNotFound", err)
	}
}

func TestAcknowledgeRequiresAnExplicitScope(t *testing.T) {
	_, s, _, _ := fixture(t)
	if err := s.Acknowledge(context.Background(), outbox.Scope{}, "alert"); err == nil {
		t.Fatal("expected an explicit-scope failure")
	}
}
