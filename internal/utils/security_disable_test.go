package utils

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSSRFDisableSwitchAcrossOutboundLayers(t *testing.T) {
	t.Setenv("SSRF_WHITELIST", "")
	t.Setenv("SSRF_WHITELIST_EXTRA", "")
	t.Setenv("SSRF_DNS_WHITELIST_ONLY", "true")
	t.Setenv("SSRF_DISABLE", "false")
	ResetSSRFWhitelistForTest()
	t.Cleanup(ResetSSRFWhitelistForTest)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "ok") }))
	defer server.Close()
	client := NewSSRFSafeHTTPClient(DefaultSSRFSafeHTTPClientConfig())
	defer client.CloseIdleConnections()
	// Prime the actual outbound rejection cache before enabling the bypass.
	if err := validateURLForSSRFForOutbound(server.URL); err == nil {
		t.Fatal("disabled switch unexpectedly bypassed SSRF")
	}
	t.Setenv("SSRF_DISABLE", "true")
	if err := ValidateURLForSSRF("http://unresolved.invalid:6379"); err != nil {
		t.Fatalf("URL validation still blocked: %v", err)
	}
	if err := CheckSSRFWhitelistOnly("unresolved.invalid"); err != nil {
		t.Fatalf("whitelist-only check still blocked: %v", err)
	}
	response, err := client.Get(server.URL)
	if err != nil {
		t.Fatalf("cache/transport/dial still blocked local HTTP: %v", err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatal(response.StatusCode)
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	conn, err := SSRFSafeDialContext(context.Background(), "tcp", listener.Addr().String())
	if err != nil {
		t.Fatalf("direct dial still blocked: %v", err)
	}
	conn.Close()
	// Bypassed successes must not poison the enabled-policy cache.
	for _, value := range []string{"", "false", "yes", "typo"} {
		t.Setenv("SSRF_DISABLE", value)
		if err := validateURLForSSRFForOutbound(server.URL); err == nil {
			t.Fatalf("SSRF_DISABLE=%q permitted outbound URL", value)
		}
	}
}

func TestSSRFDisableAllowsLocalRedirect(t *testing.T) {
	t.Setenv("SSRF_DISABLE", "true")
	t.Setenv("SSRF_WHITELIST", "")
	t.Setenv("SSRF_WHITELIST_EXTRA", "")
	t.Setenv("SSRF_DNS_WHITELIST_ONLY", "true")
	ResetSSRFWhitelistForTest()
	t.Cleanup(ResetSSRFWhitelistForTest)
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "redirected") }))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, http.StatusFound) }))
	defer source.Close()
	client := NewSSRFSafeHTTPClient(DefaultSSRFSafeHTTPClientConfig())
	defer client.CloseIdleConnections()
	response, err := client.Get(source.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil || string(body) != "redirected" {
		t.Fatalf("body=%s error=%v", body, err)
	}
}
