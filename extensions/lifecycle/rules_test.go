package lifecycle

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRulesOverrideAndRevision(t *testing.T) {
	db := database(t)
	config := NewConfiguration(db)
	ctx := context.Background()
	tenant := PolicyScope{TenantID: 7}
	kb := PolicyScope{TenantID: 7, KnowledgeBaseID: "kb-a"}
	defaults, err := config.Resolve(ctx, kb)
	if err != nil || defaults.Rules.ValidDays != 0 {
		t.Fatalf("default=%+v %v", defaults, err)
	}
	if err = config.PutRules(ctx, tenant, Rules{ValidDays: 30, ReviewDays: 7}, 0); err != nil {
		t.Fatal(err)
	}
	base, _ := config.Resolve(ctx, kb)
	if base.Rules.ValidDays != 30 {
		t.Fatal(base)
	}
	if err = config.PutRules(ctx, kb, Rules{EffectiveDelayDays: 2}, 0); err != nil {
		t.Fatal(err)
	}
	override, _ := config.Resolve(ctx, kb)
	if override.Rules.ValidDays != 0 || override.Rules.EffectiveDelayDays != 2 || override.Token == base.Token {
		t.Fatal(override)
	}
	if err = config.PutRules(ctx, kb, Rules{ValidDays: 3}, 0); !errors.Is(err, ErrConfigurationConflict) {
		t.Fatal(err)
	}
	other, _ := config.Resolve(ctx, PolicyScope{TenantID: 8, KnowledgeBaseID: "kb-a"})
	if other.Rules.ValidDays != 0 {
		t.Fatal("cross-tenant policy leak")
	}
	if err = config.PutRules(ctx, kb, Rules{ValidDays: -1}, 1); err == nil {
		t.Fatal("negative days accepted")
	}
}

func TestConcurrentAnnotationReplacementHasOneWinner(t *testing.T) {
	db := database(t)
	c := NewConfiguration(db)
	ctx := context.Background()
	s := Scope{7, "kb", "doc"}
	if err := c.PutAnnotations(ctx, s, Annotations{}, 0); err != nil {
		t.Fatal(err)
	}
	var winners atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := c.PutAnnotations(ctx, s, Annotations{Labels: map[string]string{"kind": "notice"}}, 1)
			if err == nil {
				winners.Add(1)
			} else if !errors.Is(err, ErrConfigurationConflict) {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if winners.Load() != 1 {
		t.Fatal(winners.Load())
	}
	got, err := c.Annotations(ctx, s)
	if err != nil || got.Revision != 2 {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestAnnotationDatesAndCAS(t *testing.T) {
	db := database(t)
	c := NewConfiguration(db)
	ctx := context.Background()
	s := Scope{7, "kb-a", "doc-a"}
	start := time.Date(2026, 10, 31, 9, 0, 0, 0, time.FixedZone("local", 8*3600))
	end := start.Add(time.Hour)
	a := Annotations{EffectiveFrom: &start, EffectiveUntil: &end, Labels: map[string]string{"kind": "notice"}}
	if err := c.PutAnnotations(ctx, s, a, 0); err != nil {
		t.Fatal(err)
	}
	got, err := c.Annotations(ctx, s)
	if err != nil || got.Revision != 1 || got.EffectiveFrom.Location() != time.UTC || got.Labels["kind"] != "notice" {
		t.Fatalf("%+v %v", got, err)
	}
	if err = c.PutAnnotations(ctx, s, a, 0); !errors.Is(err, ErrConfigurationConflict) {
		t.Fatal(err)
	}
	bad := a
	bad.EffectiveUntil = &start
	if err = c.PutAnnotations(ctx, s, bad, 1); err == nil {
		t.Fatal("invalid window accepted")
	}
}
