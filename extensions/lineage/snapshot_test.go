package lineage

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/ybochuan/LanRAG-Next/extensions/provenance"
	"github.com/ybochuan/LanRAG-Next/extensions/weknora"
)

func TestNativeSharedContract(t *testing.T) {
	body, e := os.ReadFile("../../contracts/bridge/v1/native-source-snapshot.json")
	if e != nil {
		t.Fatal(e)
	}
	var s Snapshot
	if e = json.Unmarshal(body, &s); e != nil {
		t.Fatal(e)
	}
	if e = s.Validate(provenance.Scope{TenantID: 7, KnowledgeBaseID: "kb"}, "doc"); e != nil {
		t.Fatal("native and extension contract differ", e)
	}
}

func TestHTTPReaderChecksFixedCredentialScopeAndCompleteFingerprint(t *testing.T) {
	snapshot := fixtureSnapshot(t, "body")
	status := 200
	redirect := ""
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/api/v1/knowledge-bases/kb/knowledge/doc/source-snapshot" || r.Header.Get("X-API-Key") != "test-credential" || r.Header.Get("X-Tenant-ID") != "7" {
			t.Error("bad authority adapter")
		}
		if redirect != "" {
			w.Header().Set("Location", redirect)
		}
		w.WriteHeader(status)
		json.NewEncoder(w).Encode(map[string]any{"success": true, "data": snapshot})
	}))
	defer srv.Close()
	client, e := weknora.NewClient(srv.URL, "test-credential", 7)
	if e != nil {
		t.Fatal(e)
	}
	r, e := NewHTTPReader(client, []string{"kb"})
	if e != nil {
		t.Fatal(e)
	}
	if got, e := r.Capture(context.Background(), fixtureScope, "doc"); e != nil || got.RootHash != snapshot.RootHash {
		t.Fatal(got, e)
	}
	if _, e = r.Capture(context.Background(), provenance.Scope{TenantID: 8, KnowledgeBaseID: "kb"}, "doc"); e == nil || calls != 1 {
		t.Fatal("foreign scope reached server")
	}
	valid := snapshot
	snapshot.RootHash = strings.Repeat("f", 64)
	if _, e = r.Capture(context.Background(), fixtureScope, "doc"); e == nil {
		t.Fatal("bad root")
	}
	snapshot = valid
	status = 302
	redirect = srv.URL + "/unexpected"
	if _, e = r.Capture(context.Background(), fixtureScope, "doc"); e == nil {
		t.Fatal("redirect followed")
	}
	status = 503
	redirect = ""
	if _, e = r.Capture(context.Background(), fixtureScope, "doc"); e == nil || strings.Contains(e.Error(), "test-credential") {
		t.Fatal(e)
	}
}

func TestSnapshotMalformedStructureRejected(t *testing.T) {
	for _, change := range []func(*Snapshot){
		func(s *Snapshot) { s.Scheme = "unrecognized" },
		func(s *Snapshot) { delete(s.HeaderFields, "custom_metadata") },
		func(s *Snapshot) { s.Chunks = append(s.Chunks, s.Chunks[0]); s.RootHash = rootDigest(*s) },
		func(s *Snapshot) { s.Chunks[0].ContentRevision = -1; s.RootHash = rootDigest(*s) },
		func(s *Snapshot) {
			s.HeaderFields["parse_status"] = sha([]byte(`"processing"`))
			s.HeaderHash = headerDigest(s.HeaderFields)
			s.RootHash = rootDigest(*s)
		},
	} {
		s := fixtureSnapshot(t, "body")
		change(&s)
		if s.Validate(fixtureScope, "doc") == nil {
			t.Fatal("malformed snapshot admitted")
		}
	}
}
