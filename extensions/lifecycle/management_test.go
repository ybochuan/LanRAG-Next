package lifecycle

import (
	"context"
	"testing"
	"time"
)

func TestManagementRejectsUnsupportedDatesAtomically(t *testing.T) {
	for _, state := range []State{Active, Scheduled} {
		t.Run(string(state), func(t *testing.T) {
			db := database(t)
			ctx := context.Background()
			s := Scope{TenantID: 7, KnowledgeBaseID: "kb", DocumentID: "doc"}
			now := time.Now().UTC()
			future := now.Add(24 * time.Hour)
			_, err := NewStore(db).Append(ctx, Event{EventID: "seed", TenantID: 7, KnowledgeBaseID: "kb", DocumentID: "doc", ToState: state, OccurredAt: now, Reason: "fixture"})
			if err != nil {
				t.Fatal(err)
			}
			c := NewConfiguration(db)
			annotation := Annotations{EffectiveFrom: &future}
			if state == Scheduled {
				annotation.EffectiveFrom = nil
			}
			if err = c.PutManagementAnnotations(ctx, s, annotation, 0, now); err == nil {
				t.Fatal("unsupported edit accepted")
			}
			got, _ := c.Annotations(ctx, s)
			if got.Revision != 0 {
				t.Fatal("rejected edit persisted")
			}
		})
	}
}
func TestManagementRulesRevisionMatchesBodyDuringConcurrentEdits(t *testing.T) {
	db := database(t)
	ctx := context.Background()
	c := NewConfiguration(db)
	s := PolicyScope{TenantID: 7, KnowledgeBaseID: "kb"}
	finished := make(chan error, 1)
	go func() {
		for n := 0; n < 60; n++ {
			if err := c.PutRules(ctx, s, Rules{ValidDays: n + 1}, int64(n)); err != nil {
				finished <- err
				return
			}
		}
		finished <- nil
	}()
	for n := 0; n < 100; n++ {
		rules, revision, err := c.ResolveForEdit(ctx, s)
		if err != nil {
			t.Fatal(err)
		}
		if int64(rules.Rules.ValidDays) != revision {
			t.Fatalf("body=%d revision=%d", rules.Rules.ValidDays, revision)
		}
	}
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
}
