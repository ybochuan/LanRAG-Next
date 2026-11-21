package bridge

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/corrections"
)

func TestCorrectionSourcesAdmission(t *testing.T) {
	key := strings.Repeat("k", 32)
	h := &CorrectionSourcesHandler{Key: key, Corrections: corrections.New(nil, time.Now)}
	now := time.Now()
	serve := func(key string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/bridge/correction-sources", strings.NewReader("{}"))
		req.Header.Set("Authorization", "Bearer "+key)
		res := httptest.NewRecorder()
		h.ServeHTTP(res, req)
		return res
	}
	for range correctionSourcesMaxInFlight {
		if !h.admit(now) {
			t.Fatal("expected concurrent slot")
		}
	}
	if res := serve("wrong"); res.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized request got %d", res.Code)
	}
	if res := serve(key); res.Code != http.StatusTooManyRequests || res.Header().Get("Retry-After") != "1" {
		t.Fatalf("saturated handler got %d / %q", res.Code, res.Header().Get("Retry-After"))
	}
	for range correctionSourcesMaxInFlight {
		h.release()
	}
	for i := correctionSourcesMaxInFlight; i < correctionSourcesPerSecond; i++ {
		if !h.admit(now) {
			t.Fatalf("window exhausted after %d requests", i)
		}
		h.release()
	}
	if h.admit(now.Add(time.Second - time.Nanosecond)) {
		t.Fatal("exhausted window admitted a request")
	}
	if !h.admit(now.Add(time.Second)) {
		t.Fatal("next window did not recover")
	}
	h.release()
}
