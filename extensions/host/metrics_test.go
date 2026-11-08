package host

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHostMetricsRendersFixedEventsAndOutboxSnapshot(t *testing.T) {
	collector := newHostMetrics()
	collector.record("reconciliation_waiting_for_core")
	collector.record("reconciliation_waiting_for_core code=upstream_unreachable consecutive=2")
	collector.record("reconciliation_failed code=reconciliation_internal_error")
	collector.record("free-form error text must not become a label")
	collector.record("projection_failed")

	handler := collector.handler(func(context.Context) (hostOutboxSnapshot, error) {
		return hostOutboxSnapshot{
			Available:         true,
			Pending:           2,
			Processing:        1,
			Dead:              4,
			OldestPending:     90 * time.Second,
			OldestPendingSeen: true,
		}, nil
	})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body := recorder.Body.String()

	for _, want := range []string{
		`host_reconciliation_events_total{event="reconciliation_waiting_for_core"} 2`,
		`host_reconciliation_events_total{event="reconciliation_failed"} 1`,
		`host_reconciliation_events_total{event="projection_failed"} 1`,
		`host_reconciliation_events_total{event="other"} 1`,
		`host_outbox_available 1`,
		`host_outbox_items{status="pending"} 2`,
		`host_outbox_items{status="processing"} 1`,
		`host_outbox_items{status="dead"} 4`,
		`host_outbox_oldest_pending_available 1`,
		`host_outbox_oldest_pending_seconds 90`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("host metrics body missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "free-form") {
		t.Fatalf("raw event text leaked into metrics: %s", body)
	}
}

func TestHostMetricsReportsUnavailableSnapshotAndQueryErrors(t *testing.T) {
	collector := newHostMetrics()
	recorder := httptest.NewRecorder()
	collector.handler(nil).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if !strings.Contains(recorder.Body.String(), "host_outbox_available 0") {
		t.Fatalf("missing unavailable marker: %s", recorder.Body.String())
	}

	recorder = httptest.NewRecorder()
	collector.handler(func(context.Context) (hostOutboxSnapshot, error) {
		return hostOutboxSnapshot{}, errors.New("outbox query failed")
	}).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body := recorder.Body.String()
	if !strings.Contains(body, "host_outbox_query_errors_total 1") {
		t.Fatalf("query error not counted: %s", body)
	}
	if !strings.Contains(body, "host_outbox_available 0") {
		t.Fatalf("error snapshot must report unavailable: %s", body)
	}
}
