package provenance

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProvenanceHTTPRejectsCredentialVersionScopeAndUnknownClaims(t *testing.T) {
	r, _ := runFixture(t)
	key := strings.Repeat("k", 32)
	s := NewHTTPServer(r, New(r.db, r.ledger.authority), key, []Scope{testScope})
	base := HTTPRequest{RequestID: "trace", Scope: testScope, Identity: &RunIdentity{Scope: testScope, UserID: "user", SessionID: "session", AttemptID: "http", AuthorizedDocumentIDs: []string{"doc"}}, OperationID: "http-begin"}
	for _, mode := range []string{"valid", "key", "version", "scope", "extra", "probe"} {
		t.Run(mode, func(t *testing.T) {
			q := base
			if mode == "scope" {
				q.Scope.TenantID++
			}
			body, _ := json.Marshal(q)
			if mode == "extra" {
				body = append(body[:len(body)-1], []byte(`,"complete":true}`)...)
			}
			req := httptest.NewRequest("POST", "/provenance/runs/begin", bytes.NewReader(body))
			req.Header.Set("Authorization", "Bearer "+key)
			req.Header.Set("X-AgentRAG-Bridge-Version", "1")
			if mode == "key" {
				req.Header.Set("Authorization", "Bearer invalid")
			}
			if mode == "version" {
				req.Header.Set("X-AgentRAG-Bridge-Version", "2")
			}
			w := httptest.NewRecorder()
			if mode == "probe" {
				NewHTTPServer(nil, nil, key, []Scope{testScope}).ServeHTTP(w, req)
			} else {
				s.ServeHTTP(w, req)
			}
			want := map[string]int{"valid": 200, "key": 401, "version": 400, "scope": 403, "extra": 400, "probe": 503}[mode]
			if w.Code != want {
				t.Fatalf("HTTP%d want%d: %s", w.Code, want, w.Body.String())
			}
			if strings.Contains(w.Body.String(), key) {
				t.Fatal("service key leaked")
			}
		})
	}
}
