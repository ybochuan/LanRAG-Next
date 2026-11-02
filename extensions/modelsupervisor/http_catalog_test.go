package modelsupervisor

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func catalogHeaders() map[string]string {
	return map[string]string{
		"X-API-Key":             testToken,
		SupervisorVersionHeader: WireVersion,
	}
}

func TestCatalogEndpointListsModels(t *testing.T) {
	server, _ := newTestServer(t)

	status, body := do(t, http.MethodGet, server.URL+"/extensions/models/catalog", nil, catalogHeaders())
	if status != http.StatusOK {
		t.Fatalf("status = %d, body = %s", status, body)
	}

	var catalog Catalog
	if err := json.Unmarshal(body, &catalog); err != nil {
		t.Fatalf("decode catalog: %v", err)
	}
	if len(catalog.Models) != 1 || catalog.Models[0].Name != validSpec().Model {
		t.Fatalf("unexpected models: %+v", catalog.Models)
	}
	if catalog.Binary != "bin/"+BinaryName {
		t.Fatalf("binary = %q", catalog.Binary)
	}
}

func TestCatalogEndpointRequiresCredential(t *testing.T) {
	server, _ := newTestServer(t)

	status, _ := do(t, http.MethodGet, server.URL+"/extensions/models/catalog", nil, map[string]string{
		SupervisorVersionHeader: WireVersion,
	})
	if status != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 without a credential", status)
	}
}

func TestCatalogEndpointUnavailableWithoutHook(t *testing.T) {
	manager, _ := newTestManager(t)
	api, err := NewServer(manager, &Prober{}, testToken)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	httpServer := httptest.NewServer(api.Handler())
	t.Cleanup(httpServer.Close)

	status, body := do(t, http.MethodGet, httpServer.URL+"/extensions/models/catalog", nil, catalogHeaders())
	if status != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 when the catalog hook is not wired; body = %s", status, body)
	}
}
