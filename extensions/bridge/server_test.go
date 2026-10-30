package bridge_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ybochuan/LanRAG-Next/extensions/bridge"
)

func TestBridgeHealthAndReadiness(t *testing.T) {
	server := httptest.NewServer(bridge.NewServer().Handler)
	t.Cleanup(server.Close)
	for _, tc := range []struct {
		path string
		want int
	}{
		{"/bridge/healthz", http.StatusOK},
		{"/healthz", http.StatusOK},
		{"/readyz", http.StatusServiceUnavailable},
	} {
		t.Run(tc.path, func(t *testing.T) {
			response, err := server.Client().Get(server.URL + tc.path)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			if response.StatusCode != tc.want {
				t.Fatalf("status = %d, want %d", response.StatusCode, tc.want)
			}
			if response.Header.Get("Content-Type") != "application/json" {
				t.Fatalf("not a JSON response: %s", response.Header.Get("Content-Type"))
			}
			if tc.want == http.StatusOK {
				var health struct{ Status string }
				if err := json.NewDecoder(response.Body).Decode(&health); err != nil || health.Status != "ok" {
					t.Fatalf("invalid liveness response: %+v, err=%v", health, err)
				}
			} else {
				assertErrorCode(t, response, "not_ready")
			}
		})
	}
}

var businessPaths = []string{
	"/bridge/before-retrieve",
	"/bridge/after-retrieve",
	"/bridge/before-generate",
	"/bridge/ingest-event",
}

func TestBridgeVersionGate(t *testing.T) {
	server := httptest.NewServer(bridge.NewServer().Handler)
	t.Cleanup(server.Close)
	for _, path := range businessPaths {
		for _, tc := range []struct {
			name   string
			values []string
			code   string
		}{
			{"missing", nil, "bridge_version_required"},
			{"empty", []string{""}, "bridge_version_required"},
			{"older", []string{"0"}, "unsupported_bridge_version"},
			{"newer", []string{"2"}, "unsupported_bridge_version"},
			{"non_major", []string{"1.0"}, "unsupported_bridge_version"},
			{"duplicate", []string{"1", "1"}, "unsupported_bridge_version"},
			{"list", []string{"1, 1"}, "unsupported_bridge_version"},
		} {
			t.Run(path+"/"+tc.name, func(t *testing.T) {
				request, err := http.NewRequest(http.MethodPost, server.URL+path, nil)
				if err != nil {
					t.Fatal(err)
				}
				for _, value := range tc.values {
					request.Header.Add("X-AgentRAG-Bridge-Version", value)
				}
				response, err := server.Client().Do(request)
				if err != nil {
					t.Fatal(err)
				}
				defer response.Body.Close()
				if response.StatusCode != http.StatusBadRequest {
					t.Fatalf("status = %d, want 400", response.StatusCode)
				}
				if response.Header.Get("X-AgentRAG-Bridge-Version") != "1" {
					t.Fatal("response must identify supported bridge version")
				}
				assertErrorCode(t, response, tc.code)
			})
		}
	}
}

func TestBridgeUnavailableServices(t *testing.T) {
	server := httptest.NewServer(bridge.NewServer().Handler)
	t.Cleanup(server.Close)
	for _, path := range businessPaths {
		t.Run(path, func(t *testing.T) {
			request, err := http.NewRequest(http.MethodPost, server.URL+path, nil)
			if err != nil {
				t.Fatal(err)
			}
			request.Header.Set("X-AgentRAG-Bridge-Version", "1")
			response, err := server.Client().Do(request)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			if response.StatusCode != http.StatusServiceUnavailable {
				t.Fatalf("unconfigured hook status = %d, want 503 (no permit or durable ACK)", response.StatusCode)
			}
			assertErrorCode(t, response, "bridge_unavailable")
		})
	}
	t.Run("wrong_method", func(t *testing.T) {
		response, err := server.Client().Get(server.URL + "/bridge/before-retrieve")
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusMethodNotAllowed {
			t.Fatalf("status = %d, want 405", response.StatusCode)
		}
	})
	t.Run("unknown_path", func(t *testing.T) {
		response, err := server.Client().Get(server.URL + "/bridge/not-registered")
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", response.StatusCode)
		}
	})
}

func assertErrorCode(t *testing.T, response *http.Response, want string) {
	t.Helper()
	var body struct {
		Error struct{ Code string } `json:"error"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Error.Code != want {
		t.Fatalf("error code = %q, want %q", body.Error.Code, want)
	}
}
