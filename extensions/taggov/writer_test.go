package taggov

import (
	"context"
	"errors"
	"runtime"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type tagStore struct {
	mu        sync.Mutex
	tags      map[string]Tag
	ids       []string
	writes    int
	active    atomic.Int32
	maximum   atomic.Int32
	failWrite bool
}

func TestResolvedTruthWaitsForPreviousWriterOperation(t *testing.T) {
	s := fixtureStore()
	w, err := NewWriter(s, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	first, second, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	defer close(release)
	results := make(chan error, 2)
	scope := Scope{7, "kb-a", "doc-a"}
	go func() {
		results <- w.ApplyResolved(context.Background(), scope, "gov:state:", func(context.Context) ([]Tag, error) { close(first); <-release; return []Tag{s.tags["old-state"]}, nil })
	}()
	<-first
	go func() {
		results <- w.ApplyResolved(context.Background(), scope, "gov:state:", func(context.Context) ([]Tag, error) { close(second); return []Tag{s.tags["active"]}, nil })
	}()
	select {
	case <-second:
		t.Fatal("resolver ran ahead of previous writer operation")
	case <-time.After(20 * time.Millisecond):
	}
	release <- struct{}{}
	for i := 0; i < 2; i++ {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	if !equalIDs(s.ids, []string{"business-a", "business-b", "finance", "active"}) {
		t.Fatalf("queue order lost: %v", s.ids)
	}
}

func (s *tagStore) enter() func() {
	n := s.active.Add(1)
	for old := s.maximum.Load(); n > old; old = s.maximum.Load() {
		if s.maximum.CompareAndSwap(old, n) {
			break
		}
	}
	runtime.Gosched()
	return func() { s.active.Add(-1) }
}

func (s *tagStore) ReadTags(context.Context, Scope) ([]Tag, error) {
	defer s.enter()()
	s.mu.Lock()
	defer s.mu.Unlock()
	var result []Tag
	for _, id := range s.ids {
		result = append(result, s.tags[id])
	}
	return result, nil
}

func (s *tagStore) ReplaceTags(_ context.Context, _ Scope, ids []string) error {
	defer s.enter()()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failWrite {
		return errors.New("upstream unavailable")
	}
	s.ids = append([]string(nil), ids...)
	s.writes++
	return nil
}

func fixtureStore() *tagStore {
	return &tagStore{
		tags: map[string]Tag{
			"business-a": {ID: "business-a", Name: "business:A"},
			"business-b": {ID: "business-b", Name: "business:B"},
			"old-state":  {ID: "old-state", Name: "gov:state:pending"},
			"active":     {ID: "active", Name: "gov:state:active"},
			"finance":    {ID: "finance", Name: "gov:domain:finance"},
		},
		ids: []string{"business-a", "business-b", "old-state", "finance"},
	}
}

func TestTagWriterPreservesBusinessAndOtherNamespaces(t *testing.T) {
	store := fixtureStore()
	writer, err := NewWriter(store, 4)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(writer.Close)
	scope := Scope{TenantID: 7, KnowledgeBaseID: "kb-a", DocumentID: "doc-a"}
	if err := writer.Apply(context.Background(), scope, "gov:state:", []Tag{store.tags["active"]}); err != nil {
		t.Fatal(err)
	}
	if got := store.ids; !equalIDs(got, []string{"business-a", "business-b", "finance", "active"}) {
		t.Fatalf("tags lost or stale namespace retained: %v", got)
	}
}

func TestTagWriterSerializesOneThousandUpdates(t *testing.T) {
	store := fixtureStore()
	writer, err := NewWriter(store, 32)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(writer.Close)
	scope := Scope{TenantID: 7, KnowledgeBaseID: "kb-a", DocumentID: "doc-a"}
	errorsFound := make(chan error, 1000)
	var workers sync.WaitGroup
	for i := 0; i < 1000; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			errorsFound <- writer.Apply(context.Background(), scope, "gov:state:", []Tag{store.tags["active"]})
		}()
	}
	workers.Wait()
	close(errorsFound)
	for err := range errorsFound {
		if err != nil {
			t.Fatal(err)
		}
	}
	if store.writes != 1000 || store.maximum.Load() != 1 {
		t.Fatalf("writes=%d concurrent API calls=%d", store.writes, store.maximum.Load())
	}
	if !equalIDs(store.ids, []string{"business-a", "business-b", "finance", "active"}) {
		t.Fatalf("business tags lost: %v", store.ids)
	}
}

func TestTagWriterReportsFailureAndRejectsAfterClose(t *testing.T) {
	store := fixtureStore()
	store.failWrite = true
	writer, err := NewWriter(store, 1)
	if err != nil {
		t.Fatal(err)
	}
	scope := Scope{TenantID: 7, KnowledgeBaseID: "kb-a", DocumentID: "doc-a"}
	if err := writer.Apply(context.Background(), scope, "gov:state:", []Tag{store.tags["active"]}); err == nil {
		t.Fatal("upstream failure must reach caller")
	}
	writer.Close()
	if err := writer.Apply(context.Background(), scope, "gov:state:", nil); !errors.Is(err, ErrClosed) {
		t.Fatalf("closed queue error = %v", err)
	}
}

func TestTagWriterRejectsNamespaceEscapeWithoutWriting(t *testing.T) {
	store := fixtureStore()
	writer, err := NewWriter(store, 1)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(writer.Close)
	scope := Scope{TenantID: 7, KnowledgeBaseID: "kb-a", DocumentID: "doc-a"}
	if err := writer.Apply(context.Background(), scope, "gov:state:", []Tag{store.tags["finance"]}); err == nil {
		t.Fatal("another namespace must not be accepted")
	}
	if store.writes != 0 {
		t.Fatal("invalid operation reached upstream")
	}
}

func equalIDs(a, b []string) bool {
	a, b = slices.Clone(a), slices.Clone(b)
	slices.Sort(a)
	slices.Sort(b)
	return slices.Equal(a, b)
}
