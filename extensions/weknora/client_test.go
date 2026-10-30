package weknora

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestClientHeadersAndSafeErrors(t *testing.T) {
	for _, scenario := range []string{"ok", "denied", "bad", "oversized", "timeout"} {
		t.Run(scenario, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("X-API-Key") != "secret" || r.Header.Get("X-Tenant-ID") != "7" {
					t.Error("missing credential/scope headers")
				}
				switch scenario {
				case "denied":
					w.WriteHeader(403)
					fmt.Fprint(w, "secret response body")
				case "bad":
					fmt.Fprint(w, `{"success":true}garbage`)
				case "oversized":
					fmt.Fprintf(w, `{"success":true,"data":%q}`, strings.Repeat("x", 4*1024*1024))
				case "timeout":
					<-r.Context().Done()
				default:
					fmt.Fprint(w, `{"success":true}`)
				}
			}))
			defer s.Close()
			client, err := NewClient(s.URL, "secret", 7)
			if err != nil {
				t.Fatal(err)
			}
			timeout := 5 * time.Second
			if scenario == "timeout" {
				timeout = 50 * time.Millisecond
			}
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()
			var result struct{ Success bool }
			err = client.JSON(ctx, http.MethodGet, "/api/v1/test", nil, nil, &result)
			if scenario == "ok" {
				if err != nil || !result.Success {
					t.Fatalf("result=%+v %v", result, err)
				}
				return
			}
			if err == nil || strings.Contains(err.Error(), "secret") {
				t.Fatalf("unsafe error=%v", err)
			}
			if scenario == "oversized" && !strings.Contains(err.Error(), "size limit") {
				t.Fatalf("size guard not exercised: %v", err)
			}
		})
	}
}

func TestClientNeverFollowsCredentialRedirects(t *testing.T) {
	var calls atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); t.Error("redirect reached target") }))
	defer target.Close()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 302) }))
	defer s.Close()
	c, err := NewClient(s.URL, "secret", 7)
	if err != nil {
		t.Fatal(err)
	}
	var result any
	if err := c.JSON(context.Background(), "GET", "/api/v1/test", nil, nil, &result); err == nil || calls.Load() != 0 {
		t.Fatalf("redirect err=%v calls=%d", err, calls.Load())
	}
	for _, base := range []string{"http://user:secret@localhost", "http://localhost/?token=secret", "http://localhost:8080?", "http://localhost/#secret", "http://192.168.1.2:8080"} {
		if _, err := NewClient(base, "secret", 7); err == nil {
			t.Fatalf("unsafe base accepted")
		}
	}
}
