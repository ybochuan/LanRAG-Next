package contract_test

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/ybochuan/LanRAG-Next/extensions/contract"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("../../contracts/bridge/v1/" + name + ".json")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// Losing a frozen field or converting metadata numbers through float64 breaks
// the wire round trip; expectations are literal data shared with future clients.
func TestRequestFixturePreservesPublicContextAndMetadata(t *testing.T) {
	input := fixture(t, "before-retrieve.request")
	req, err := contract.DecodeBeforeRetrieve(input)
	if err != nil {
		t.Fatal(err)
	}
	if req.RequestID != "request-1" || req.SessionID != "session-1" || req.Mode != "agent" || req.AgentRound != 2 || req.TenantID != 7 || req.TokenBudget != 800 || req.OriginalQuery != "source query" || req.RewrittenQuery != "rewritten query" {
		t.Fatalf("public context lost: %+v", req)
	}
	if !reflect.DeepEqual(req.KnowledgeBaseIDs, []string{"kb-a", "kb-b"}) || !reflect.DeepEqual(req.KnowledgeIDs, []string{"doc-a", "doc-b", "doc-c"}) || !reflect.DeepEqual(req.TagScopes, map[string][]string{"kb-a": {"tag-business"}}) {
		t.Fatalf("scope lost: %+v", req)
	}
	encoded, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(encoded, []byte("9007199254740993")) {
		t.Fatal("metadata integer changed")
	}
	var a, b map[string]json.RawMessage
	if err := json.Unmarshal(input, &a); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(encoded, &b); err != nil {
		t.Fatal(err)
	}
	if len(a) != len(b) {
		t.Fatalf("field count %d != %d", len(a), len(b))
	}
	for key, value := range a {
		var x, y any
		left, right := json.NewDecoder(bytes.NewReader(value)), json.NewDecoder(bytes.NewReader(b[key]))
		left.UseNumber()
		right.UseNumber()
		if err := left.Decode(&x); err != nil {
			t.Fatal(err)
		}
		if err := right.Decode(&y); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(x, y) {
			t.Fatalf("field %s changed", key)
		}
	}
}

func TestExplicitEmptyDocumentScopeRemainsDistinctFromUnknown(t *testing.T) {
	input := fixture(t, "before-retrieve.empty")
	got, err := contract.DecodeBeforeRetrieve(input)
	if err != nil || got.KnowledgeIDs == nil || len(got.KnowledgeIDs) != 0 {
		t.Fatalf("explicit empty scope lost: %+v %v", got, err)
	}
	for _, replacement := range []string{"null", ""} {
		broken := strings.Replace(string(input), `"knowledge_ids": [],`, `"knowledge_ids": null,`, 1)
		if replacement == "" {
			broken = strings.Replace(string(input), `  "knowledge_ids": [],`, "", 1)
		}
		if _, err := contract.DecodeBeforeRetrieve([]byte(broken)); err == nil {
			t.Fatal("unknown scope accepted")
		}
	}
}

func TestInvalidScopeAndContextRejected(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*contract.BeforeRetrieveRequest)
	}{
		{"tenant", func(r *contract.BeforeRetrieveRequest) { r.TenantID = 0 }},
		{"empty_kb", func(r *contract.BeforeRetrieveRequest) { r.KnowledgeBaseIDs = []string{} }},
		{"duplicate_kb", func(r *contract.BeforeRetrieveRequest) { r.KnowledgeBaseIDs = []string{"kb-a", "kb-a"} }},
		{"bad_kb", func(r *contract.BeforeRetrieveRequest) { r.KnowledgeBaseIDs = []string{"../kb"} }},
		{"empty_doc", func(r *contract.BeforeRetrieveRequest) { r.KnowledgeIDs = []string{""} }},
		{"duplicate_doc", func(r *contract.BeforeRetrieveRequest) { r.KnowledgeIDs = []string{"a", "a"} }},
		{"bad_doc", func(r *contract.BeforeRetrieveRequest) { r.KnowledgeIDs = []string{"a\nb"} }},
		{"huge_doc_id", func(r *contract.BeforeRetrieveRequest) { r.KnowledgeIDs = []string{strings.Repeat("x", 257)} }},
		{"too_many_docs", func(r *contract.BeforeRetrieveRequest) { r.KnowledgeIDs = make([]string, 10001) }},
		{"foreign_tag_scope", func(r *contract.BeforeRetrieveRequest) { r.TagScopes = map[string][]string{"kb-foreign": {"tag-a"}} }},
		{"unknown_tag_list", func(r *contract.BeforeRetrieveRequest) { r.TagScopes = map[string][]string{"kb-a": nil} }},
		{"bad_tag", func(r *contract.BeforeRetrieveRequest) { r.TagScopes = map[string][]string{"kb-a": {"tag/a"}} }},
		{"round", func(r *contract.BeforeRetrieveRequest) { r.AgentRound = -1 }},
		{"budget", func(r *contract.BeforeRetrieveRequest) { r.TokenBudget = -1 }},
		{"mode", func(r *contract.BeforeRetrieveRequest) { r.Mode = "other" }},
		{"request_id", func(r *contract.BeforeRetrieveRequest) { r.RequestID = " " }},
		{"session_id", func(r *contract.BeforeRetrieveRequest) { r.SessionID = "" }},
		{"huge_query", func(r *contract.BeforeRetrieveRequest) { r.OriginalQuery = strings.Repeat("x", 65537) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, err := contract.DecodeBeforeRetrieve(fixture(t, "before-retrieve.request"))
			if err != nil {
				t.Fatal(err)
			}
			tc.change(&r)
			if err := r.Validate(); err == nil {
				t.Fatal("invalid request accepted")
			}
		})
	}
}

func TestMalformedOrAmbiguousWireInputRejected(t *testing.T) {
	base := string(fixture(t, "before-retrieve.request"))
	for name, input := range map[string]string{
		"null": "null", "array": "[]", "empty": "", "truncated": "{",
		"trailing": base + `{}`, "unknown": strings.Replace(base, `"mode":`, `"future":1,"mode":`, 1),
		"alias":             strings.Replace(base, `"tenant_id"`, `"TENANT_ID"`, 1),
		"duplicate":         strings.Replace(base, `"tenant_id": 7`, `"tenant_id":8,"tenant_id":7`, 1),
		"escaped_duplicate": strings.Replace(base, `"tenant_id": 7`, `"tenant_\u0069d":8,"tenant_id":7`, 1),
		"nested_duplicate":  strings.Replace(base, `"counter": 9007199254740993`, `"counter":1,"counter":2`, 1),
		"wrong_tenant_type": strings.Replace(base, `"tenant_id": 7`, `"tenant_id":"7"`, 1),
		"overflow_tenant":   strings.Replace(base, `"tenant_id": 7`, `"tenant_id":18446744073709551616`, 1),
		"null_budget":       strings.Replace(base, `"token_budget": 800`, `"token_budget":null`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := contract.DecodeBeforeRetrieve([]byte(input)); err == nil {
				t.Fatal("ambiguous JSON accepted")
			}
		})
	}
}
