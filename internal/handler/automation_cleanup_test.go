package handler

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestCleanupRecipientsIncludesSharedTenantsAndDeduplicates(t *testing.T) {
	h := &AutomationHandler{}
	req := reviewAlertNotification{TenantID: 1, KnowledgeBaseID: "kb", Title: "cleanup"}
	kb := &types.KnowledgeBase{TenantID: 1, CreatorID: "owner"}
	shared := func(context.Context, uint64, string) []cleanupRecipient {
		return []cleanupRecipient{
			{tenant: 1, user: "owner"}, // duplicate owner
			{tenant: 2, user: "member-a"},
			{tenant: 2, user: "member-a"}, // duplicate
			{tenant: 3, user: ""},         // invalid
			{tenant: 0, user: "member-c"}, // invalid
			{tenant: 4, user: "member-d"},
		}
	}
	got := h.cleanupRecipients(context.Background(), req, kb, shared)
	if len(got) != 3 {
		t.Fatalf("recipients=%+v", got)
	}
	want := []cleanupRecipient{{tenant: 1, user: "owner"}, {tenant: 2, user: "member-a"}, {tenant: 4, user: "member-d"}}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("recipient[%d]=%+v want %+v", i, got[i], want[i])
		}
	}
}

func TestCleanupRecipientsOwnerOnlyWithoutResolver(t *testing.T) {
	h := &AutomationHandler{}
	req := reviewAlertNotification{TenantID: 1, KnowledgeBaseID: "kb"}
	kb := &types.KnowledgeBase{TenantID: 1, CreatorID: "owner"}
	got := h.cleanupRecipients(context.Background(), req, kb, nil)
	if len(got) != 1 || got[0] != (cleanupRecipient{tenant: 1, user: "owner"}) {
		t.Fatalf("recipients=%+v", got)
	}
}
