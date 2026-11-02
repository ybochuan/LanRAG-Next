package bridge

import (
	"context"
	"net/http/httptest"
	"testing"
)

func TestLocalReadinessAliases(t *testing.T) {
	for _, ready := range []bool{false, true} {
		server := NewServerWithServices(Services{Ready: func(context.Context) bool { return ready }})
		for _, path := range []string{"/health", "/readyz"} {
			response := httptest.NewRecorder()
			server.Handler.ServeHTTP(response, httptest.NewRequest("GET", path, nil))
			expected := 503
			if ready {
				expected = 200
			}
			if response.Code != expected {
				t.Fatalf("ready=%v path=%s code=%d", ready, path, response.Code)
			}
		}
	}
}
