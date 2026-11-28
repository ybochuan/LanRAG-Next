package taggov

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

type stubCatalog struct {
	tags []Tag
	err  error
}

func (s stubCatalog) Catalog(context.Context, Scope) ([]Tag, error) { return s.tags, s.err }

func TestPreconditionerReportsMissingLifecycleStateTags(t *testing.T) {
	p := NewPreconditioner(stubCatalog{tags: []Tag{{"1", "gov:state:active"}, {"2", "gov:state:stale"}}})
	missing, err := p.MissingLifecycleStateTags(context.Background(), 7, "kb-a")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"gov:state:pending", "gov:state:scheduled"}
	if !reflect.DeepEqual(missing, want) {
		t.Fatalf("missing=%v", missing)
	}
}

func TestPreconditionerDoesNotMaskCatalogFailure(t *testing.T) {
	failure := errors.New("catalog unavailable")
	_, err := NewPreconditioner(stubCatalog{err: failure}).MissingLifecycleStateTags(context.Background(), 7, "kb-a")
	if !errors.Is(err, failure) {
		t.Fatalf("err=%v", err)
	}
}
