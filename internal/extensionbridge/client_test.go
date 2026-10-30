package extensionbridge

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Route the production fixed address to a test-owned real HTTP server. No
// listener or process already using the user's port 8090 is contacted.
type testRoute struct {
	t           *testing.T
	destination *url.URL
	transport   http.RoundTripper
}

func (rt testRoute) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.String() != "http://127.0.0.1:8090/bridge/before-retrieve" {
		rt.t.Errorf("unexpected bridge destination: %s", r.URL)
	}
	copy := r.Clone(r.Context())
	u := *r.URL
	u.Scheme, u.Host = rt.destination.Scheme, rt.destination.Host
	copy.URL = &u
	return rt.transport.RoundTrip(copy)
}

func routeTo(t *testing.T, destination string) {
	t.Helper()
	u, err := url.Parse(destination)
	if err != nil {
		t.Fatal(err)
	}
	transport := &http.Transport{Proxy: nil}
	original := bridgeHTTPClient
	copy := *original
	copy.Transport = testRoute{t, u, transport}
	bridgeHTTPClient = &copy
	t.Cleanup(func() { bridgeHTTPClient = original; transport.CloseIdleConnections() })
}

func TestBeforeRetrievePostsContextAndRejectsHTTPFailure(t *testing.T) {
	for _, status := range []int{200, 400, 503, 500, 302, 204} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var calls atomic.Int32
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.Method != http.MethodPost || r.URL.Path != "/bridge/before-retrieve" || r.Header.Get("Content-Type") != "application/json" || r.Header.Get("X-AgentRAG-Bridge-Version") != "1" {
					t.Errorf("wrong request: %s %s %v", r.Method, r.URL.Path, r.Header)
				}
				var payload struct {
					RequestID string `json:"request_id"`
					TenantID  uint64 `json:"tenant_id"`
				}
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil || payload.RequestID != "request-1" || payload.TenantID != 7 {
					t.Errorf("payload lost: %+v %v", payload, err)
				}
				if status == 302 {
					w.Header().Set("Location", "/unexpected")
				}
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`{"details":"private backend content"}`))
			}))
			t.Cleanup(s.Close)
			routeTo(t, s.URL)
			err := BeforeRetrieve(context.Background(), map[string]any{"request_id": "request-1", "tenant_id": 7})
			if (err == nil) != (status == 200) || calls.Load() != 1 {
				t.Fatalf("status %d: err=%v calls=%d", status, err, calls.Load())
			}
			if err != nil && strings.Contains(err.Error(), "private backend content") {
				t.Fatal("error leaked response body")
			}
		})
	}
}

func TestBeforeRetrieveUnavailableTimeoutAndCancellation(t *testing.T) {
	t.Run("host_stopped", func(t *testing.T) {
		s := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		destination := s.URL
		s.Close()
		routeTo(t, destination)
		if err := BeforeRetrieve(context.Background(), struct{}{}); err == nil {
			t.Fatal("stopped Host permitted retrieval")
		}
	})
	t.Run("500ms_timeout", func(t *testing.T) {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			select {
			case <-r.Context().Done():
			case <-time.After(2 * time.Second):
				w.WriteHeader(200)
			}
		}))
		t.Cleanup(s.Close)
		routeTo(t, s.URL)
		start := time.Now()
		err := BeforeRetrieve(context.Background(), struct{}{})
		var timeout net.Error
		if err == nil || !errors.As(err, &timeout) || !timeout.Timeout() || time.Since(start) > 1500*time.Millisecond {
			t.Fatalf("500ms deadline not enforced: %v duration=%s", err, time.Since(start))
		}
	})
	t.Run("canceled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := BeforeRetrieve(ctx, struct{}{}); !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation lost: %v", err)
		}
	})
	t.Run("bad_payload", func(t *testing.T) {
		if err := BeforeRetrieve(context.Background(), make(chan int)); err == nil {
			t.Fatal("unencodable payload permitted")
		}
	})
}
