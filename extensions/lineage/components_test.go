package lineage

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/lifecycle"
	"github.com/ybochuan/LanRAG-Next/extensions/provenance"
	"github.com/ybochuan/LanRAG-Next/extensions/store"
	"github.com/ybochuan/LanRAG-Next/extensions/weknora"
)

func componentFixture(t *testing.T) Components {
	t.Helper()
	s := fixtureSnapshot(t, "body")
	fields := map[string]string{}
	for _, name := range chunkFieldNames {
		fields[name] = sha([]byte("null"))
	}
	fields["content"] = sha([]byte(`"body"`))
	s.Chunks[0].FieldsHash = headerDigest(fields)
	s.RootHash = rootDigest(s)
	return Components{View: "components-v1", Snapshot: s, ChunkFields: map[string]map[string]string{"chunk": fields}}
}

func TestTrustedRunHTTPProducerAdmissionRevokeAndRestart(t *testing.T) {
	ctx := context.Background()
	registry, db, reader, file := registryFixture(t)
	view := componentFixture(t)
	reader.snapshot = view.Snapshot
	var nativeOnline atomic.Bool
	nativeOnline.Store(true)
	native := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !nativeOnline.Load() {
			w.WriteHeader(503)
			return
		}
		var data any = view.Snapshot
		if r.URL.Query().Get("view") == "components-v1" {
			data = view
		}
		json.NewEncoder(w).Encode(map[string]any{"success": true, "data": data})
	}))
	defer native.Close()
	client, e := weknora.NewClient(native.URL, "fixture-machine-key", 7)
	if e != nil {
		t.Fatal(e)
	}
	httpReader, e := NewHTTPReader(client, []string{"kb"})
	if e != nil {
		t.Fatal(e)
	}
	registry = NewRegistry(db, httpReader)
	key := strings.Repeat("k", 32)
	runs := provenance.NewRuns(db, NewComponentVerifier(httpReader, registry), NewAuthority(registry, nil), []byte(key))
	ledger := provenance.New(db, NewAuthority(registry, runs))
	host := httptest.NewServer(provenance.NewHTTPServer(runs, ledger, key, []provenance.Scope{fixtureScope}))
	defer host.Close()
	call := func(path string, q provenance.HTTPRequest, want int, out any) {
		t.Helper()
		q.RequestID = "request"
		q.Scope = fixtureScope
		body, _ := json.Marshal(q)
		req, e := http.NewRequest("POST", host.URL+path, bytes.NewReader(body))
		if e != nil {
			t.Fatal(e)
		}
		req.Header.Set("Authorization", "Bearer "+key)
		req.Header.Set("X-AgentRAG-Bridge-Version", "1")
		response, e := http.DefaultClient.Do(req)
		if e != nil {
			t.Fatal(e)
		}
		defer response.Body.Close()
		encoded, e := io.ReadAll(response.Body)
		if e != nil {
			t.Fatal(e)
		}
		if response.StatusCode != want {
			t.Fatalf("%s HTTP%d expected%d", path, response.StatusCode, want)
		}
		if want == 200 && out != nil {
			var env struct {
				Success   bool             `json:"success"`
				RequestID string           `json:"request_id"`
				Scope     provenance.Scope `json:"scope"`
				Data      json.RawMessage  `json:"data"`
			}
			if json.Unmarshal(encoded, &env) != nil || !env.Success || env.RequestID != q.RequestID || env.Scope != fixtureScope || json.Unmarshal(env.Data, out) != nil {
				t.Fatal("invalid protocol response")
			}
		}
	}
	identity := provenance.RunIdentity{Scope: fixtureScope, UserID: "user", SessionID: "session", AttemptID: "attempt", AuthorizedDocumentIDs: []string{"doc"}}
	var h provenance.RunHandle
	call("/provenance/runs/begin", provenance.HTTPRequest{Identity: &identity, OperationID: "begin"}, 200, &h)
	var model provenance.StepHandle
	call("/provenance/runs/steps", provenance.HTTPRequest{Handle: &h, Action: "begin", Step: &provenance.StepRequest{OperationID: "model", Kind: "model"}}, 200, &model)
	call("/provenance/runs/inputs", provenance.HTTPRequest{Handle: &h, Input: &provenance.InputRequest{OperationID: "input", StepID: model.StepID, Evidence: provenance.InputEvidence{DocumentID: "doc", ChunkID: "chunk", Field: "content", ValueHash: sha([]byte(`"body"`))}}}, 200, nil)
	call("/provenance/runs/steps", provenance.HTTPRequest{Handle: &h, Action: "finish", Result: &provenance.StepResult{OperationID: "model-done", StepID: model.StepID, Outcome: "success", InputHash: sha([]byte("fixture encoded model input")), OutputHash: sha([]byte("fixture model output"))}}, 200, nil)
	var write provenance.StepHandle
	call("/provenance/runs/steps", provenance.HTTPRequest{Handle: &h, Action: "begin", Step: &provenance.StepRequest{OperationID: "write", Kind: "write"}}, 200, &write)
	binding := provenance.Binding{UserID: "user", SessionID: "session", ResourceID: "resource", ContentVersion: "v1", SHA256: sha([]byte("derived file bytes"))}
	var producer provenance.ProducerHandle
	call("/provenance/producers", provenance.HTTPRequest{Handle: &h, Action: "reserve", Producer: &provenance.ProducerRequest{OperationID: "reserve", StepID: write.StepID, CopyID: "copy", Path: "result.md", Binding: binding}}, 200, &producer)
	call("/provenance/copies/resolve", provenance.HTTPRequest{Binding: &binding}, 403, nil)
	call("/provenance/producers", provenance.HTTPRequest{Handle: &h, Action: "commit", Produced: &provenance.ProducerResult{OperationID: "commit", ProducerID: producer.ProducerID, StepID: write.StepID, Binding: binding}}, 200, nil)
	call("/provenance/runs/steps", provenance.HTTPRequest{Handle: &h, Action: "finish", Result: &provenance.StepResult{OperationID: "write-done", StepID: write.StepID, Outcome: "success", InputHash: sha([]byte("write args")), OutputHash: binding.SHA256}}, 200, nil)
	var snapshot provenance.RunSnapshot
	call("/provenance/runs/finish", provenance.HTTPRequest{Handle: &h, Action: "inspect"}, 200, &snapshot)
	call("/provenance/runs/finish", provenance.HTTPRequest{Handle: &h, Finish: &provenance.FinishRequest{OperationID: "seal", ExpectedRevision: snapshot.Revision}}, 200, &snapshot)
	call("/provenance/producers", provenance.HTTPRequest{Handle: &h, Action: "publish", Save: &provenance.SaveRequest{Scope: fixtureScope, OperationID: "publish", CopyID: "copy", Binding: binding, EvidenceID: producer.ProducerID}}, 200, nil)
	var manifest provenance.Manifest
	call("/provenance/copies/resolve", provenance.HTTPRequest{Binding: &binding}, 200, &manifest)
	read := provenance.ReadRequest{Scope: fixtureScope, CopyID: manifest.CopyID, Revision: manifest.Revision, Binding: binding}
	check := provenance.HTTPRequest{Read: &read, AuthorizedDocumentIDs: []string{"doc"}}
	call("/provenance/copies/check", check, 200, nil)
	call("/provenance/copies/check", provenance.HTTPRequest{Read: &read}, 403, nil)
	nativeOnline.Store(false)
	call("/provenance/copies/check", check, 403, nil)
	nativeOnline.Store(true)
	_, e = lifecycle.NewStore(db).Append(ctx, lifecycle.Event{EventID: "stale", TenantID: 7, KnowledgeBaseID: "kb", DocumentID: "doc", FromState: lifecycle.Active, ToState: lifecycle.Stale, Reason: "fixture revoke", OccurredAt: time.Now()})
	if e != nil {
		t.Fatal(e)
	}
	call("/provenance/copies/check", check, 403, nil)
	host.Close()
	if e = db.Close(); e != nil {
		t.Fatal(e)
	}
	db2, e := store.OpenDurable(ctx, file)
	if e != nil {
		t.Fatal(e)
	}
	defer db2.Close()
	registry2 := NewRegistry(db2, httpReader)
	runs2 := provenance.NewRuns(db2, NewComponentVerifier(httpReader, registry2), NewAuthority(registry2, nil), []byte(key))
	ledger2 := provenance.New(db2, NewAuthority(registry2, runs2))
	host = httptest.NewServer(provenance.NewHTTPServer(runs2, ledger2, key, []provenance.Scope{fixtureScope}))
	defer host.Close()
	call("/provenance/runs/finish", provenance.HTTPRequest{Handle: &h, Action: "inspect"}, 200, &snapshot)
	if snapshot.State != "sealed" {
		t.Fatal("restart lost sealed fact")
	}
	call("/provenance/copies/check", check, 403, nil)
}

func TestComponentVerifierChecksActualFieldsAndCompleteFold(t *testing.T) {
	registry, db, reader, _ := registryFixture(t)
	view := componentFixture(t)
	reader.snapshot = view.Snapshot
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("view") != "components-v1" || r.URL.Query().Get("chunk_ids") != "chunk" {
			t.Error("component selection missing")
		}
		json.NewEncoder(w).Encode(map[string]any{"success": true, "data": view})
	}))
	defer srv.Close()
	client, e := weknora.NewClient(srv.URL, "test-key", 7)
	if e != nil {
		t.Fatal(e)
	}
	httpReader, e := NewHTTPReader(client, []string{"kb"})
	if e != nil {
		t.Fatal(e)
	}
	v := NewComponentVerifier(httpReader, registry)
	evidence := provenance.InputEvidence{DocumentID: "doc", ChunkID: "chunk", Field: "content", ValueHash: sha([]byte(`"body"`))}
	refs, e := v.VerifyInput(context.Background(), fixtureScope, evidence)
	if e != nil || len(refs) != 1 {
		t.Fatalf("valid proof: %v", e)
	}
	// Use the real run consumer: no test source authority is installed.
	runs := provenance.NewRuns(db, v, NewAuthority(registry, nil), []byte("01234567890123456789012345678901"))
	h, e := runs.Begin(context.Background(), provenance.RunIdentity{Scope: fixtureScope, UserID: "user", SessionID: "session", AttemptID: "attempt", AuthorizedDocumentIDs: []string{"doc"}}, "begin")
	if e != nil {
		t.Fatal(e)
	}
	s, e := runs.BeginStep(context.Background(), h, provenance.StepRequest{OperationID: "step", Kind: "model"})
	if e != nil {
		t.Fatal(e)
	}
	if e = runs.RecordInput(context.Background(), h, provenance.InputRequest{OperationID: "input", StepID: s.StepID, Evidence: evidence}); e != nil {
		t.Fatal("real source admission", e)
	}
	evidence.ValueHash = sha([]byte(`"rewritten or merged"`))
	if _, e = v.VerifyInput(context.Background(), fixtureScope, evidence); e == nil {
		t.Fatal("rewritten field admitted")
	}
	evidence.ValueHash = sha([]byte(`"body"`))
	view.ChunkFields["chunk"]["context_header"] = sha([]byte(`"tampered"`))
	if _, e = v.VerifyInput(context.Background(), fixtureScope, evidence); e == nil {
		t.Fatal("partial field map admitted")
	}
	view = componentFixture(t)
	reader.snapshot = fixtureSnapshot(t, "native changed")
	if _, e = v.VerifyInput(context.Background(), fixtureScope, evidence); e == nil {
		t.Fatal("old component root admitted after native change")
	}
}
