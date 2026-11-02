package extensionbridge

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBeforeRetrieveStrictDecisionAndPartition(t *testing.T) {
	valid := `{"request_id":"r","tenant_id":7,"knowledge_base_ids":["kb"],"knowledge_ids":["active"],"excluded_document_ids":["stale"],"deny_all":false}`
	request := map[string]any{"request_id": "r", "tenant_id": 7, "knowledge_base_ids": []string{"kb"}, "knowledge_ids": []string{"active", "stale"}}
	for _, tc := range []struct {
		name, body string
		ok         bool
	}{
		{"valid", valid, true}, {"empty", "", false}, {"missing", "{}", false}, {"trailing", valid + ` {}`, false}, {"wrong_tenant", strings.Replace(valid, `"tenant_id":7`, `"tenant_id":8`, 1), false},
		{"wrong_trace", strings.Replace(valid, `"request_id":"r"`, `"request_id":"other"`, 1), false}, {"wrong_kb", strings.Replace(valid, `["kb"]`, `["foreign"]`, 1), false},
		{"outside_ids", strings.Replace(valid, `["stale"]`, `["foreign"]`, 1), false}, {"null_ids", strings.Replace(valid, `["stale"]`, `null`, 1), false},
		{"missing_exclusion", strings.Replace(valid, `["stale"]`, `[]`, 1), false}, {"overlap", strings.Replace(valid, `["stale"]`, `["active"]`, 1), false},
		{"wrong_deny", strings.Replace(valid, `false`, `true`, 1), false}, {"duplicate", strings.Replace(valid, `"deny_all":false`, `"deny_all":true,"deny_all":false`, 1), false},
		{"wrong_case", strings.Replace(valid, `"deny_all"`, `"DENY_ALL"`, 1), false}, {"oversized", strings.Repeat(" ", 1<<20) + valid, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(tc.body)) }))
			defer s.Close()
			routeTo(t, s.URL)
			decision, err := BeforeRetrieve(context.Background(), request)
			if (err == nil) != tc.ok {
				t.Fatalf("decision=%+v error=%v", decision, err)
			}
			if tc.ok && (len(decision.ExcludedDocumentIDs) != 1 || decision.ExcludedDocumentIDs[0] != "stale") {
				t.Fatal(decision)
			}
		})
	}
}
