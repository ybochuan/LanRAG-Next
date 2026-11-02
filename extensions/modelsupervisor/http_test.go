package modelsupervisor

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

const testToken = "test-token"

func newTestServer(t *testing.T) (*httptest.Server, *Manager) {
	t.Helper()
	manager, _ := newTestManager(t)
	server, err := NewServer(manager, &Prober{}, testToken)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	// Capacity is stubbed so the transport tests never depend on the GPU this
	// machine happens to be using.
	server.Resources = func() Resources {
		return Resources{State: ResourceOK, FreeGB: 100, TotalGB: 200, ReserveGB: ReserveGB}
	}
	server.Catalog = func() (Catalog, error) {
		return Catalog{
			ModelDir:  "models",
			BinaryDir: "bin",
			Binary:    "bin/" + BinaryName,
			Models:    []ModelFile{{Name: validSpec().Model, SizeBytes: 1024}},
		}, nil
	}
	httpServer := httptest.NewServer(server.Handler())
	t.Cleanup(httpServer.Close)
	return httpServer, manager
}

func do(t *testing.T, method, url string, body any, headers map[string]string) (int, []byte) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		reader = bytes.NewReader(raw)
	}
	request, err := http.NewRequest(method, url, reader)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	request.Header.Set("Content-Type", "application/json")
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	defer func() { _ = response.Body.Close() }()
	payload, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return response.StatusCode, payload
}

func authorized() map[string]string {
	return map[string]string{"X-API-Key": testToken}
}

func TestHealthzIsUnauthenticated(t *testing.T) {
	server, _ := newTestServer(t)
	status, body := do(t, http.MethodGet, server.URL+"/healthz", nil, nil)
	if status != http.StatusOK {
		t.Fatalf("status = %d, body = %s", status, body)
	}
}

func TestProtectedRoutesRequireCredential(t *testing.T) {
	server, _ := newTestServer(t)
	cases := []struct {
		name    string
		headers map[string]string
	}{
		{"no credential", nil},
		{"wrong key", map[string]string{"X-API-Key": "nope"}},
		{"wrong bearer", map[string]string{"Authorization": "Bearer nope"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, body := do(t, http.MethodGet, server.URL+"/extensions/models/list", nil, tc.headers)
			if status != http.StatusUnauthorized {
				t.Fatalf("status = %d, body = %s", status, body)
			}
		})
	}
}

func TestCredentialAcceptedAsHeaderOrBearer(t *testing.T) {
	server, _ := newTestServer(t)
	cases := []map[string]string{
		{"X-API-Key": testToken},
		{"Authorization": "Bearer " + testToken},
	}
	for _, headers := range cases {
		status, body := do(t, http.MethodGet, server.URL+"/extensions/models/list", nil, headers)
		if status != http.StatusOK {
			t.Fatalf("status = %d, body = %s", status, body)
		}
	}
}

func TestUnsupportedVersionIsRejected(t *testing.T) {
	server, _ := newTestServer(t)
	headers := authorized()
	headers[SupervisorVersionHeader] = "2"
	status, body := do(t, http.MethodGet, server.URL+"/extensions/models/list", nil, headers)
	if status != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", status, body)
	}
}

func TestStartReturnsConflictForDuplicateID(t *testing.T) {
	server, manager := newTestServer(t)
	if _, err := manager.Start(validSpec()); err != nil {
		t.Fatalf("seed start: %v", err)
	}
	status, body := do(t, http.MethodPost, server.URL+"/extensions/models/start", validSpec(), authorized())
	if status != http.StatusConflict {
		t.Fatalf("status = %d, body = %s", status, body)
	}
}

// A CPU-only instance consumes RAM, not VRAM, so a short VRAM budget must not
// block it. Gating it there refuses a start that cannot exhaust the card.
func TestStartSkipsCapacityCheckForCPUOnlyInstance(t *testing.T) {
	manager, _ := newTestManager(t)
	server, err := NewServer(manager, &Prober{}, testToken)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	server.Resources = func() Resources {
		return Resources{State: ResourceOK, FreeGB: 0.1, TotalGB: 24, ReserveGB: ReserveGB}
	}
	httpServer := httptest.NewServer(server.Handler())
	t.Cleanup(httpServer.Close)

	noGPU := false
	status, body := do(t, http.MethodPost, httpServer.URL+"/extensions/models/start", map[string]any{
		"id":        "cpu-chat",
		"model":     validSpec().Model,
		"task_type": "chat",
		"port":      8083,
		"use_gpu":   noGPU,
	}, authorized())
	if status != http.StatusAccepted {
		t.Fatalf("status = %d, body = %s", status, body)
	}
}

func TestStartReturnsPreconditionRequiredWhenCapacityIsShort(t *testing.T) {
	manager, _ := newTestManager(t)
	server, err := NewServer(manager, &Prober{}, testToken)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	server.Resources = func() Resources {
		return Resources{State: ResourceOK, FreeGB: 0.1, TotalGB: 24, ReserveGB: ReserveGB}
	}
	httpServer := httptest.NewServer(server.Handler())
	t.Cleanup(httpServer.Close)

	status, body := do(t, http.MethodPost, httpServer.URL+"/extensions/models/start", validSpec(), authorized())
	if status != http.StatusPreconditionRequired {
		t.Fatalf("status = %d, body = %s", status, body)
	}
	var decoded struct {
		Code        string  `json:"code"`
		EstimatedGB float64 `json:"estimated_gb"`
		FreeGB      float64 `json:"free_gb"`
		ReserveGB   float64 `json:"reserve_gb"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("decode: %v (%s)", err, body)
	}
	if decoded.Code != "capacity_exceeded" || decoded.FreeGB != 0.1 || decoded.ReserveGB != ReserveGB {
		t.Fatalf("decoded = %+v", decoded)
	}
	if decoded.EstimatedGB <= 0 {
		t.Fatalf("estimated_gb = %.2f, want the projection to be reported", decoded.EstimatedGB)
	}

	// An explicit override must succeed and say so.
	forced := map[string]any{"id": "embedding", "model": validSpec().Model, "task_type": "embedding", "force": true}
	status, body = do(t, http.MethodPost, httpServer.URL+"/extensions/models/start", forced, authorized())
	if status != http.StatusAccepted {
		t.Fatalf("forced status = %d, body = %s", status, body)
	}
	if !bytes.Contains(body, []byte(`"forced":true`)) {
		t.Fatalf("body = %s, want the override to be recorded", body)
	}
}

func TestStartRejectsUnknownModel(t *testing.T) {
	server, _ := newTestServer(t)
	payload := map[string]any{"id": "embedding", "model": "absent.gguf", "task_type": "embedding"}
	status, body := do(t, http.MethodPost, server.URL+"/extensions/models/start", payload, authorized())
	if status != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", status, body)
	}
}

func TestStartRejectsTraversal(t *testing.T) {
	server, _ := newTestServer(t)
	payload := map[string]any{"id": "embedding", "model": "../secrets.gguf", "task_type": "embedding"}
	status, body := do(t, http.MethodPost, server.URL+"/extensions/models/start", payload, authorized())
	if status != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", status, body)
	}
}

func TestStopUnknownInstanceIsNotFound(t *testing.T) {
	server, _ := newTestServer(t)
	status, body := do(t, http.MethodPost, server.URL+"/extensions/models/stop", map[string]any{"id": "ghost"}, authorized())
	if status != http.StatusNotFound {
		t.Fatalf("status = %d, body = %s", status, body)
	}
}

func TestStopIsIdempotent(t *testing.T) {
	server, manager := newTestServer(t)
	if _, err := manager.Start(validSpec()); err != nil {
		t.Fatalf("start: %v", err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		status, body := do(t, http.MethodPost, server.URL+"/extensions/models/stop", map[string]any{"id": validSpec().ID}, authorized())
		if status != http.StatusOK {
			t.Fatalf("attempt %d status = %d, body = %s", attempt, status, body)
		}
	}
}

func TestLogsReturnsLinesAndCursor(t *testing.T) {
	server, manager := newTestServer(t)
	if _, err := manager.Start(validSpec()); err != nil {
		t.Fatalf("start: %v", err)
	}
	// The drain goroutine races the first request; wait for output to land.
	waitFor(t, "helper output", func() bool {
		lines, _, err := manager.Logs(validSpec().ID, 0)
		return err == nil && len(lines) > 0
	})
	status, body := do(t, http.MethodGet, server.URL+"/extensions/models/"+validSpec().ID+"/logs", nil, authorized())
	if status != http.StatusOK {
		t.Fatalf("status = %d, body = %s", status, body)
	}
	var decoded struct {
		Lines []LogLine `json:"lines"`
		Next  uint64    `json:"next"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("decode: %v (%s)", err, body)
	}
	if decoded.Next == 0 {
		t.Fatalf("next cursor = %d, want a non-zero cursor after a start", decoded.Next)
	}
}

func TestLogsRejectsBadCursor(t *testing.T) {
	server, _ := newTestServer(t)
	status, body := do(t, http.MethodGet, server.URL+"/extensions/models/embedding/logs?since=abc", nil, authorized())
	if status != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", status, body)
	}
}

func TestProbeWithNoInstancesIsEmpty(t *testing.T) {
	server, _ := newTestServer(t)
	status, body := do(t, http.MethodGet, server.URL+"/extensions/models/probe", nil, authorized())
	if status != http.StatusOK {
		t.Fatalf("status = %d, body = %s", status, body)
	}
	var decoded struct {
		Instances     []any `json:"instances"`
		ObservedPorts []int `json:"observed_ports"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("decode: %v (%s)", err, body)
	}
	if len(decoded.Instances) != 0 {
		t.Fatalf("instances = %d", len(decoded.Instances))
	}
}

// A port that is already bound is a 409 with its own code, not a 202 that turns
// into "failed" a second later.
func TestStartPortInUseIsConflict(t *testing.T) {
	manager, _ := newTestManagerWith(t, helperLaunch, func(int) error {
		return fmt.Errorf("%w: address already in use", ErrPortInUse)
	})
	server, err := NewServer(manager, &Prober{}, testToken)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	server.Resources = func() Resources {
		return Resources{State: ResourceOK, FreeGB: 100, TotalGB: 200, ReserveGB: ReserveGB}
	}
	httpServer := httptest.NewServer(server.Handler())
	t.Cleanup(httpServer.Close)

	status, body := do(t, http.MethodPost, httpServer.URL+"/extensions/models/start", validSpec(), authorized())
	if status != http.StatusConflict {
		t.Fatalf("status = %d, body = %s", status, body)
	}
	var payload struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload.Code != "port_in_use" {
		t.Fatalf("code = %q, want port_in_use", payload.Code)
	}
}
