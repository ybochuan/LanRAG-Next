package bridge

import (
	"context"
	"errors"
	"github.com/ybochuan/LanRAG-Next/extensions/contract"
	"net/http/httptest"
	"strings"
	"testing"
)

type attachmentPolicyFixture struct {
	fail  bool
	calls int
}

func (p *attachmentPolicyFixture) AttachmentLinks(context.Context, contract.BeforeRetrieveRequest) (map[string][]contract.RelatedAttachment, error) {
	p.calls++
	if p.fail {
		return nil, errors.New("private source details")
	}
	return map[string][]contract.RelatedAttachment{}, nil
}
func TestAttachmentEnrichmentRequiresCredentialVersionAndPolicy(t *testing.T) {
	key := strings.Repeat("k", 32)
	for _, scenario := range []string{"ok", "no_key", "bad_version", "policy_error"} {
		t.Run(scenario, func(t *testing.T) {
			p := &attachmentPolicyFixture{fail: scenario == "policy_error"}
			s := NewServerWithServices(Services{AfterRetrieve: &AttachmentHandler{Key: key, Policy: p}})
			r := httptest.NewRequest("POST", "/bridge/after-retrieve", strings.NewReader(`{"request_id":"r","session_id":"s","mode":"rag","tenant_id":7,"knowledge_base_ids":["kb-a"],"knowledge_ids":["parent"]}`))
			r.Header.Set(VersionHeader, Version)
			r.Header.Set("Authorization", "Bearer "+key)
			if scenario == "no_key" {
				r.Header.Del("Authorization")
			}
			if scenario == "bad_version" {
				r.Header.Set(VersionHeader, "2")
			}
			w := httptest.NewRecorder()
			s.Handler.ServeHTTP(w, r)
			if scenario == "ok" {
				if w.Code != 200 || p.calls != 1 {
					t.Fatal("valid enrichment rejected")
				}
			} else if w.Code == 200 || strings.Contains(w.Body.String(), "private source details") {
				t.Fatal("invalid request/policy error leaked")
			}
			if (scenario == "no_key" || scenario == "bad_version") && p.calls != 0 {
				t.Fatal("unauthorized enrichment reached policy")
			}
		})
	}
}
