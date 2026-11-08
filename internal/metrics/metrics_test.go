package metrics

import (
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRegistryRendersCoreAndSQLiteMetrics(t *testing.T) {
	registry := New()
	registry.ObserveHTTP("get", "/api/v1/items/:id", 200, 12*time.Millisecond)
	registry.ObserveHTTP("GET", "/api/v1/items/:id", 404, 75*time.Millisecond)
	registry.ObserveSQLite("SELECT", 250*time.Millisecond, nil)
	registry.ObserveSQLite("select", 10*time.Millisecond, errors.New("SQLITE_BUSY: database is locked"))
	registry.ObserveSQLite("update", 5*time.Millisecond, errors.New("SQLITE_LOCKED: table is locked"))
	registry.ObserveRAGStage("chunk_rerank", 2500*time.Millisecond, "success")
	registry.ObserveRAGStage("chunk_search", 10*time.Millisecond, "empty")
	registry.ObserveRAGStage("chunk_merge", 15*time.Millisecond, "error")
	registry.ObserveRAGStage("unknown-stage", time.Second, "other")
	registry.SetDBStatsProvider(func() DBStats {
		return DBStats{
			OpenConnections: 1,
			InUse:           1,
			Idle:            0,
			WaitCount:       3,
			WaitDuration:    1500 * time.Millisecond,
		}
	})

	recorder := httptest.NewRecorder()
	registry.ServeHTTP(recorder, httptest.NewRequest("GET", "/metrics", nil))
	body := recorder.Body.String()

	for _, want := range []string{
		`core_http_requests_total{method="GET",route="/api/v1/items/:id",status_class="2xx"} 1`,
		`core_http_requests_total{method="GET",route="/api/v1/items/:id",status_class="4xx"} 1`,
		`core_http_request_duration_seconds_count{method="GET",route="/api/v1/items/:id"} 2`,
		`sqlite_slow_queries_total{operation="select"} 1`,
		`sqlite_busy_total{operation="select"} 1`,
		`sqlite_locked_total{operation="update"} 1`,
		`rag_stage_duration_seconds_count{stage="chunk_rerank"} 1`,
		`rag_stage_duration_seconds_bucket{stage="chunk_rerank",le="2.5"} 1`,
		`rag_stage_errors_total{stage="chunk_merge"} 1`,
		`rag_stage_empty_total{stage="chunk_search"} 1`,
		`rag_stage_duration_seconds_count{stage="other"} 1`,
		`sqlite_conn_stats_available 1`,
		`sqlite_conn_open_total 1`,
		`sqlite_conn_wait_total 3`,
		`sqlite_conn_wait_duration_seconds 1.5`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("metrics body missing %q:\n%s", want, body)
		}
	}
}

func TestRegistryReportsUnavailableDBStats(t *testing.T) {
	registry := New()
	recorder := httptest.NewRecorder()
	registry.ServeHTTP(recorder, httptest.NewRequest("GET", "/metrics", nil))
	if !strings.Contains(recorder.Body.String(), "sqlite_conn_stats_available 0") {
		t.Fatalf("missing unavailable marker: %s", recorder.Body.String())
	}
}

func TestStatusClassAndRouteNormalization(t *testing.T) {
	registry := New()
	registry.ObserveHTTP("", "", 0, time.Millisecond)
	recorder := httptest.NewRecorder()
	registry.ServeHTTP(recorder, httptest.NewRequest("GET", "/metrics", nil))
	body := recorder.Body.String()
	if !strings.Contains(body, `method="OTHER",route="unmatched",status_class="unknown"`) {
		t.Fatalf("unexpected normalized labels: %s", body)
	}
}
