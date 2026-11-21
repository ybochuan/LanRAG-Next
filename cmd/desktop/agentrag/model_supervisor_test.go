package agentrag

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
)

func TestDesktopModelSupervisorProxyKeepsKeyInGo(t *testing.T) {
	var called atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called.Add(1)
		if r.URL.RequestURI() != "/extensions/models/qa/logs?since=12" ||
			r.Header.Get("X-API-Key") != "child-key" ||
			r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
			t.Error("expected the model path and child credential only")
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	target, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	supervisor := NewModelSupervisor(t.TempDir(), "")
	supervisor.proxy.Store(modelSupervisorProxy(target, "child-key"))
	req := httptest.NewRequest(http.MethodGet, "/extensions/models/qa/logs?since=12", nil)
	req.Header.Set("Authorization", "Bearer core")
	req.Header.Set("Cookie", "core-session=private")
	rec := httptest.NewRecorder()
	supervisor.ServeHTTP(rec, req)
	if rec.Code != 403 || called.Load() != 0 {
		t.Fatal("a request without the model protocol header reached the child")
	}
	req.Header.Set(supervisorVersionHeader, "1")
	rec = httptest.NewRecorder()
	supervisor.ServeHTTP(rec, req)
	if rec.Code != 200 || called.Load() != 1 {
		t.Errorf("proxy status=%d child calls=%d", rec.Code, called.Load())
	}
}
