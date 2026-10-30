// Package taggov serializes Extension-originated tag projections.
package taggov

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
)

// ErrClosed reports an unaccepted or interrupted queue operation at shutdown.
var ErrClosed = errors.New("tag projection writer closed")

// Scope identifies the upstream document and its owning tenant/KB.
type Scope struct {
	TenantID        uint64
	KnowledgeBaseID string
	DocumentID      string
}

// Tag must be resolved through the KB-local tag catalog before projection.
// Names select the governed namespace; only native IDs are written upstream.
type Tag struct {
	ID   string
	Name string
}

// Store is implemented by an authenticated public API adapter, not a core import.
type Store interface {
	ReadTags(context.Context, Scope) ([]Tag, error)
	ReplaceTags(context.Context, Scope, []string) error
}

type update struct {
	ctx       context.Context
	scope     Scope
	namespace string
	tags      []Tag
	resolve   func(context.Context) ([]Tag, error)
	result    chan error
}

// Writer owns one in-memory worker for all Extension-originated projections.
// It cannot serialize native UI writers and is not a durable Outbox replacement.
type Writer struct {
	store Store
	jobs  chan update
	stop  chan struct{}
	done  chan struct{}
	once  sync.Once
}

// NewWriter starts a bounded queue. The application must share a single instance.
func NewWriter(store Store, capacity int) (*Writer, error) {
	if store == nil || capacity < 0 {
		return nil, fmt.Errorf("tag writer requires a store and non-negative capacity")
	}
	w := &Writer{store: store, jobs: make(chan update, capacity), stop: make(chan struct{}), done: make(chan struct{})}
	go w.run()
	return w, nil
}

// Apply queues a complete read/merge/replace/verify operation for one namespace.
// A cancellation or error must remain retryable by the future durable Outbox.
func (w *Writer) Apply(ctx context.Context, scope Scope, namespace string, tags []Tag) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if scope.TenantID == 0 || scope.KnowledgeBaseID == "" || scope.DocumentID == "" || !validNamespace(namespace) {
		return fmt.Errorf("invalid tag projection scope or namespace")
	}
	for _, tag := range tags {
		if tag.ID == "" || !strings.HasPrefix(tag.Name, namespace) {
			return fmt.Errorf("projection tag must have a native ID in the target namespace")
		}
	}
	job := update{ctx: ctx, scope: scope, namespace: namespace, tags: append([]Tag(nil), tags...), result: make(chan error, 1)}
	return w.enqueue(job)
}

// ApplyResolved resolves current truth and native IDs inside the same worker
// slot as the read/merge/replace/verify operation, never ahead of queued jobs.
func (w *Writer) ApplyResolved(ctx context.Context, scope Scope, namespace string, resolve func(context.Context) ([]Tag, error)) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !validScope(scope) || !validNamespace(namespace) || resolve == nil {
		return fmt.Errorf("invalid resolved tag projection")
	}
	return w.enqueue(update{ctx: ctx, scope: scope, namespace: namespace, resolve: resolve, result: make(chan error, 1)})
}

func (w *Writer) enqueue(job update) error {
	ctx := job.ctx
	select {
	case <-w.stop:
		return ErrClosed
	default:
	}
	select {
	case <-w.stop:
		return ErrClosed
	case <-ctx.Done():
		return ctx.Err()
	case w.jobs <- job:
	}
	select {
	case err := <-job.result:
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-w.done:
		select {
		case err := <-job.result:
			return err
		default:
			return ErrClosed
		}
	}
}

// Close stops accepting jobs and waits for the current operation to finish.
// Call it from the service owner, outside any Store callback; Store honors contexts.
func (w *Writer) Close() {
	w.once.Do(func() { close(w.stop) })
	<-w.done
}

func (w *Writer) run() {
	defer close(w.done)
	for {
		select {
		case <-w.stop:
			return
		case job := <-w.jobs:
			if err := job.ctx.Err(); err != nil {
				job.result <- err
				continue
			}
			job.result <- w.apply(job)
		}
	}
}

func (w *Writer) apply(job update) error {
	if job.resolve != nil {
		var err error
		job.tags, err = job.resolve(job.ctx)
		if err != nil {
			return err
		}
		for _, tag := range job.tags {
			if tag.ID == "" || !strings.HasPrefix(tag.Name, job.namespace) {
				return fmt.Errorf("resolved tag outside target namespace")
			}
		}
	}
	current, err := w.store.ReadTags(job.ctx, job.scope)
	if err != nil {
		return fmt.Errorf("read tags: %w", err)
	}
	ids := make([]string, 0, len(current)+len(job.tags))
	seen := make(map[string]bool)
	for _, tag := range current {
		if tag.ID == "" {
			return fmt.Errorf("upstream tag has no native ID")
		}
		if !strings.HasPrefix(tag.Name, job.namespace) && !seen[tag.ID] {
			ids = append(ids, tag.ID)
			seen[tag.ID] = true
		}
	}
	for _, tag := range job.tags {
		if !seen[tag.ID] {
			ids = append(ids, tag.ID)
			seen[tag.ID] = true
		}
	}
	if err := w.store.ReplaceTags(job.ctx, job.scope, ids); err != nil {
		return fmt.Errorf("replace tags: %w", err)
	}
	verified, err := w.store.ReadTags(job.ctx, job.scope)
	if err != nil {
		return fmt.Errorf("verify tags: %w", err)
	}
	actual := make([]string, 0, len(verified))
	for _, tag := range verified {
		actual = append(actual, tag.ID)
	}
	if !sameIDs(actual, ids) {
		return fmt.Errorf("tag projection verification mismatch")
	}
	return nil
}

func validNamespace(namespace string) bool {
	switch namespace {
	case "gov:state:", "gov:domain:", "gov:family:", "gov:relation:", "gov:parse:":
		return true
	default:
		return false
	}
}

func sameIDs(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	counts := make(map[string]int, len(a))
	for _, id := range a {
		counts[id]++
	}
	for _, id := range b {
		if counts[id] == 0 {
			return false
		}
		counts[id]--
	}
	return true
}
