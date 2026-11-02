package modelsupervisor

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const consoleOrigin = "http://127.0.0.1:18880"

// newCORSServer builds a server whose only difference from newTestServer is the
// origin allowlist, so the CORS tests exercise the real middleware.
func newCORSServer(t *testing.T, origins []string) *httptest.Server {
	t.Helper()
	manager, _ := newTestManager(t)
	server, err := NewServer(manager, &Prober{}, testToken)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	server.AllowedOrigins = origins
	httpServer := httptest.NewServer(server.Handler())
	t.Cleanup(httpServer.Close)
	return httpServer
}

func preflight(t *testing.T, url, origin string) *http.Response {
	t.Helper()
	request, err := http.NewRequest(http.MethodOptions, url+"/extensions/models/list", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	request.Header.Set("Origin", origin)
	request.Header.Set("Access-Control-Request-Method", "GET")
	request.Header.Set("Access-Control-Request-Headers", "X-API-Key")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("preflight: %v", err)
	}
	t.Cleanup(func() { _ = response.Body.Close() })
	return response
}

// A preflight never carries the token, so it must be answered before the
// credential guard runs; otherwise the browser reports a CORS failure and the
// operator sees "network error" instead of "unauthorized".
func TestCORSPreflightFromConsoleOriginSucceeds(t *testing.T) {
	httpServer := newCORSServer(t, []string{consoleOrigin})
	response := preflight(t, httpServer.URL, consoleOrigin)

	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("preflight status = %d, want %d", response.StatusCode, http.StatusNoContent)
	}
	if got := response.Header.Get("Access-Control-Allow-Origin"); got != consoleOrigin {
		t.Fatalf("allow-origin = %q, want %q", got, consoleOrigin)
	}
	if got := response.Header.Get("Access-Control-Allow-Headers"); !strings.Contains(got, "X-API-Key") {
		t.Fatalf("allow-headers = %q, want it to include X-API-Key", got)
	}
}

// An origin that is not the console must not be able to drive local model
// processes from a page the operator happens to have open.
func TestCORSPreflightFromUnknownOriginIsRefused(t *testing.T) {
	httpServer := newCORSServer(t, []string{consoleOrigin})
	response := preflight(t, httpServer.URL, "http://evil.example")

	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("preflight status = %d, want %d", response.StatusCode, http.StatusForbidden)
	}
	if got := response.Header.Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("allow-origin = %q, want it to be absent", got)
	}
}

// With no allowlist the supervisor behaves exactly as it did before the console
// existed: no browser may call it.
func TestCORSDisabledWhenNoOriginsConfigured(t *testing.T) {
	httpServer := newCORSServer(t, nil)
	response := preflight(t, httpServer.URL, consoleOrigin)

	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("preflight status = %d, want %d", response.StatusCode, http.StatusForbidden)
	}
}

// A plain cross-origin GET is not preflighted, so the headers have to be on the
// real response too.
func TestCORSHeadersAppearOnActualResponse(t *testing.T) {
	httpServer := newCORSServer(t, []string{consoleOrigin})
	request, err := http.NewRequest(http.MethodGet, httpServer.URL+"/healthz", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	request.Header.Set("Origin", consoleOrigin)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.StatusCode)
	}
	if got := response.Header.Get("Access-Control-Allow-Origin"); got != consoleOrigin {
		t.Fatalf("allow-origin = %q, want %q", got, consoleOrigin)
	}
}

// The console refreshes one card at a time; probing every instance for a single
// card would spawn one WSL curl per configured model on each refresh.
func TestProbeOneReportsASingleInstance(t *testing.T) {
	httpServer, manager := newTestServer(t)
	spec := Spec{ID: "probe-me", Model: validSpec().Model, TaskType: TaskEmbedding, Port: 65500, CtxSize: 4096, Threads: 8}
	if _, err := manager.Start(spec); err != nil {
		t.Fatalf("start: %v", err)
	}

	status, payload := do(t, http.MethodGet, httpServer.URL+"/extensions/models/probe-me/probe", nil,
		map[string]string{"X-API-Key": testToken})
	if status != http.StatusOK {
		t.Fatalf("status = %d, body = %s", status, payload)
	}
	body := string(payload)
	if !strings.Contains(body, `"id":"probe-me"`) {
		t.Fatalf("body = %s, want it to name probe-me", body)
	}
	// Nothing listens on 65500, so the probe must stop at the first layer and say
	// so rather than claiming the instance is reachable.
	if !strings.Contains(body, `"reachability":"unverified"`) {
		t.Fatalf("body = %s, want unverified", body)
	}
}

func TestProbeOneUnknownInstanceIs404(t *testing.T) {
	httpServer, _ := newTestServer(t)
	status, payload := do(t, http.MethodGet, httpServer.URL+"/extensions/models/ghost/probe", nil,
		map[string]string{"X-API-Key": testToken})
	if status != http.StatusNotFound {
		t.Fatalf("status = %d, want 404, body = %s", status, payload)
	}
}
