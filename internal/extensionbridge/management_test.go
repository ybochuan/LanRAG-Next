package extensionbridge

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type managementRoundTrip func(*http.Request) (*http.Response, error)

func (f managementRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestManagementBindsResponseAndHidesCredential(t *testing.T) {
	t.Setenv("AGENTRAG_MANAGEMENT_KEY", strings.Repeat("s", 32))
	old := managementHTTPClient.Transport
	defer func() { managementHTTPClient.Transport = old }()
	for _, foreign := range []bool{false, true} {
		managementHTTPClient.Transport = managementRoundTrip(func(r *http.Request) (*http.Response, error) {
			if r.URL.String() != "http://127.0.0.1:8090/management/lifecycle" || r.Header.Get("X-AgentRAG-Bridge-Version") != "1" || r.Header.Get("Authorization") != "Bearer "+strings.Repeat("s", 32) {
				t.Fatal("invalid bridge authority")
			}
			tenant := "7"
			if foreign {
				tenant = "8"
			}
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"success":true,"tenant_id":` + tenant + `,"knowledge_base_id":"kb","data":{"can_write":false}}`))}, nil
		})
		_, err := ManageLifecycle(context.Background(), ManagementRequest{Action: "overview", TenantID: 7, KnowledgeBaseID: "kb"})
		if (err != nil) != foreign {
			t.Fatalf("foreign=%v err=%v", foreign, err)
		}
	}
}

func TestManagementRejectsMalformedOfflineAndTimeout(t *testing.T) {
	t.Setenv("AGENTRAG_MANAGEMENT_KEY", strings.Repeat("s", 32))
	original := managementHTTPClient
	defer func() { managementHTTPClient = original }()
	for _, response := range []struct {
		status int
		body   string
	}{{503, `{}`}, {200, `not-json`}, {200, `{"success":true,"tenant_id":7,"knowledge_base_id":"other","data":{}}`}} {
		managementHTTPClient = &http.Client{Timeout: 2 * time.Second, Transport: managementRoundTrip(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: response.status, Body: io.NopCloser(strings.NewReader(response.body))}, nil
		})}
		_, err := ManageLifecycle(context.Background(), ManagementRequest{TenantID: 7, KnowledgeBaseID: "kb"})
		if err == nil || ManagementStatus(err) != 503 {
			t.Fatal("invalid upstream response accepted")
		}
	}
	managementHTTPClient = &http.Client{Timeout: 20 * time.Millisecond, Transport: managementRoundTrip(func(r *http.Request) (*http.Response, error) { <-r.Context().Done(); return nil, r.Context().Err() })}
	started := time.Now()
	_, err := ManageLifecycle(context.Background(), ManagementRequest{TenantID: 7, KnowledgeBaseID: "kb"})
	if err == nil || ManagementStatus(err) != 503 || time.Since(started) > time.Second {
		t.Fatal("timeout did not fail closed")
	}
}
