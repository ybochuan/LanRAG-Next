package management

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type fakeTagHealth struct {
	missing []string
	err     error
}

func (f fakeTagHealth) MissingLifecycleStateTags(context.Context, uint64, string) ([]string, error) {
	return f.missing, f.err
}

func TestGovernanceHealthReturnsTagAndOutboxState(t *testing.T) {
	s := fixture(t).WithTagHealth(fakeTagHealth{missing: []string{"gov:state:active"}})
	w := call(s, `{"action":"governance_health","tenant_id":7,"knowledge_base_id":"kb"}`, s.key)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"blocked":0`) || !strings.Contains(w.Body.String(), "gov:state:active") {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := call(s, `{"action":"retry_blocked","tenant_id":7,"knowledge_base_id":"kb"}`, s.key); w.Code != 403 {
		t.Fatalf("read-only retry status=%d", w.Code)
	}
	if w := call(s, `{"action":"retry_blocked","tenant_id":7,"knowledge_base_id":"kb","can_write":true}`, s.key); w.Code != 200 || !strings.Contains(w.Body.String(), `"retried":0`) {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestGovernanceHealthSurfacesCatalogFailure(t *testing.T) {
	s := fixture(t).WithTagHealth(fakeTagHealth{err: errors.New("catalog unavailable")})
	w := call(s, `{"action":"governance_health","tenant_id":7,"knowledge_base_id":"kb"}`, s.key)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "tag_catalog_unavailable") {
		t.Fatal(w.Code, w.Body.String())
	}
}
