package agentrag

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestBootstrapUsesOnlyLocalKnowledgeBases(t *testing.T) {
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "POST /api/v1/auth/auto-setup":
			if r.Header.Get("X-WeKnora-Desktop-Token") != "native-test-token" {
				t.Error("native setup capability missing")
			}
			json.NewEncoder(w).Encode(map[string]any{"token": "test-session", "user": map[string]any{"tenant_id": 7}})
		case "GET /api/v1/knowledge-bases":
			if r.Header.Get("X-API-Key") != "" {
				json.NewEncoder(w).Encode(map[string]any{"success": true, "data": []any{map[string]any{"id": "kb-a", "tenant_id": 7}, map[string]any{"id": "kb-b", "tenant_id": 7}}})
				return
			}
			if r.Header.Get("Authorization") != "Bearer test-session" {
				t.Error("native session missing")
			}
			json.NewEncoder(w).Encode(map[string]any{"success": true, "data": []any{
				map[string]any{"id": "kb-b", "tenant_id": 7},
				map[string]any{"id": "other-tenant", "tenant_id": 8},
				map[string]any{"id": "kb-a", "tenant_id": 7},
			}})
		case "POST /api/v1/tenants/7/api-keys":
			requests++
			var input struct {
				FullAccess bool     `json:"full_access"`
				IDs        []string `json:"knowledge_base_ids"`
				Caps       []string `json:"capabilities"`
			}
			if err := json.NewDecoder(r.Body).Decode(&input); err != nil || input.FullAccess ||
				!reflect.DeepEqual(input.IDs, []string{"kb-a", "kb-b"}) || !reflect.DeepEqual(input.Caps, []string{"retrieve", "ingest", "manage_kbs"}) {
				t.Errorf("unexpected machine key scope: %+v, %v", input, err)
			}
			json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{"id": 32, "token": "test-machine-key"}})
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := newCoreClient(server.URL, "native-test-token")
	tenant, ids, err := client.scope(context.Background())
	if err != nil || tenant != 7 || !reflect.DeepEqual(ids, []string{"kb-a", "kb-b"}) {
		t.Fatalf("unexpected scope: %d %v %v", tenant, ids, err)
	}
	s := state{}
	if changed, err := client.bind(context.Background(), &s, tenant, ids); err != nil || !changed {
		t.Fatalf("issuing scoped key: changed=%t err=%v", changed, err)
	}
	if changed, err := client.bind(context.Background(), &s, tenant, ids); err != nil || changed || requests != 1 {
		t.Fatalf("reusing scoped key: changed=%t requests=%d err=%v", changed, requests, err)
	}
}

func TestSavedDesktopStateKeepsSecretsAndScope(t *testing.T) {
	dir := t.TempDir()
	first, err := loadState(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Management) < 32 || len(first.BeforeGenerate) < 32 || len(first.Corrections) < 32 ||
		first.Management == first.BeforeGenerate || first.Management == first.Corrections || first.BeforeGenerate == first.Corrections {
		t.Fatal("governance credentials must be independent")
	}
	first.Tenant = 7
	first.KBs = []string{"kb-a"}
	if err := saveState(dir, first); err != nil {
		t.Fatal(err)
	}
	second, err := loadState(dir)
	if err != nil || !reflect.DeepEqual(first, second) {
		t.Fatalf("reopening profile changed its state: %v", err)
	}
}

func TestDesktopMCPProvisioningReusesLocalService(t *testing.T) {
	var created, refreshed int
	const key = "desktop-host-auth-fixture"
	svc := hostMCPService{ID: "mcp-a", TenantID: 7, Name: hostMCPName, URL: hostMCPURL, Transport: "http-streamable", Enabled: true}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-session" || r.Header.Get("X-API-Key") != "" {
			t.Error("expected the Desktop owner session")
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.Method + " " + r.URL.Path {
		case "GET /api/v1/mcp-services":
			var data []hostMCPService
			if created > 0 {
				data = append(data, svc)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": data})
		case "POST /api/v1/mcp-services":
			var input struct {
				AuthConfig map[string]string `json:"auth_config"`
			}
			if json.NewDecoder(r.Body).Decode(&input) != nil || input.AuthConfig["auth_type"] != "bearer" || input.AuthConfig["token"] != key {
				t.Error("expected the native bearer credential field")
			}
			created++
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": svc})
		case "POST /api/v1/mcp-services/mcp-a/metadata/refresh":
			refreshed++
			_, _ = w.Write([]byte(`{"success":true,"synced":true,"data":{"tools":[{"name":"query_related_docs"},{"name":"search_archived_documents"}]}}`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	c := newCoreClient(server.URL, "")
	c.token, c.tenant = "test-session", 7
	for range 2 {
		if err := c.syncHostMCP(context.Background(), key); err != nil {
			t.Fatal(err)
		}
	}
	if created != 1 || refreshed != 2 {
		t.Errorf("expected one registration and two refreshes, got %d and %d", created, refreshed)
	}
}
