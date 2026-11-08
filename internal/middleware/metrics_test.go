package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/metrics"
	"github.com/gin-gonic/gin"
)

func TestMetricsMiddlewareRecordsRoutePattern(t *testing.T) {
	gin.SetMode(gin.TestMode)
	metrics.Default.Reset()
	t.Cleanup(metrics.Default.Reset)

	engine := gin.New()
	engine.Use(Metrics())
	engine.GET("/api/v1/items/:id", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/items/123?token=secret", nil)
	engine.ServeHTTP(recorder, request)

	metricsRecorder := httptest.NewRecorder()
	metrics.Default.ServeHTTP(metricsRecorder, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body := metricsRecorder.Body.String()
	if !strings.Contains(body, `route="/api/v1/items/:id"`) {
		t.Fatalf("route pattern missing: %s", body)
	}
	if strings.Contains(body, "secret") || strings.Contains(body, "/123") {
		t.Fatalf("raw URL leaked into metrics: %s", body)
	}
}

func TestMetricsEndpointOnlyAllowsLoopback(t *testing.T) {
	gin.SetMode(gin.TestMode)
	metrics.Default.Reset()
	t.Cleanup(metrics.Default.Reset)

	engine := gin.New()
	engine.GET("/metrics", MetricsEndpoint())

	loopback := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	loopback.RemoteAddr = "127.0.0.1:39001"
	loopbackRecorder := httptest.NewRecorder()
	engine.ServeHTTP(loopbackRecorder, loopback)
	if loopbackRecorder.Code != http.StatusOK {
		t.Fatalf("loopback status = %d", loopbackRecorder.Code)
	}

	remote := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	remote.RemoteAddr = "203.0.113.9:39001"
	remoteRecorder := httptest.NewRecorder()
	engine.ServeHTTP(remoteRecorder, remote)
	if remoteRecorder.Code != http.StatusNotFound {
		t.Fatalf("remote status = %d, want 404", remoteRecorder.Code)
	}

	forwarded := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	forwarded.RemoteAddr = "127.0.0.1:39001"
	forwarded.Header.Set("X-Forwarded-For", "203.0.113.9")
	forwardedRecorder := httptest.NewRecorder()
	engine.ServeHTTP(forwardedRecorder, forwarded)
	if forwardedRecorder.Code != http.StatusNotFound {
		t.Fatalf("forwarded status = %d, want 404", forwardedRecorder.Code)
	}
}
