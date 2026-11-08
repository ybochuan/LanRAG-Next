package middleware

import (
	"net"
	"net/http"
	"time"

	"github.com/Tencent/WeKnora/internal/metrics"
	"github.com/gin-gonic/gin"
)

// Metrics records Core HTTP RED metrics after each request. The route label is
// the registered Gin route pattern, never the raw URL or query string.
func Metrics() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.FullPath() == "/metrics" {
			c.Next()
			return
		}
		start := time.Now()
		c.Next()
		metrics.Default.ObserveHTTP(c.Request.Method, c.FullPath(), c.Writer.Status(), time.Since(start))
	}
}

// MetricsEndpoint exposes the process-local metrics registry only to loopback
// clients. It deliberately rejects proxy-forwarded requests; the endpoint is a
// local operational surface, not a public API.
func MetricsEndpoint() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.GetHeader("X-Forwarded-For") != "" || c.GetHeader("X-Real-IP") != "" {
			c.Status(http.StatusNotFound)
			return
		}
		host, _, err := net.SplitHostPort(c.Request.RemoteAddr)
		if err != nil {
			c.Status(http.StatusNotFound)
			return
		}
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() {
			c.Status(http.StatusNotFound)
			return
		}
		metrics.Default.ServeHTTP(c.Writer, c.Request)
	}
}
