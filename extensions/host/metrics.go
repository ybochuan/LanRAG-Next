package host

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/outbox"
)

// hostMetrics keeps only fixed event codes and aggregate outbox counts. It
// intentionally never stores raw report error strings, tenant IDs, or KB IDs.
type hostMetrics struct {
	mu           sync.Mutex
	events       map[string]uint64
	outboxErrors uint64
}

type hostOutboxSnapshot struct {
	Available         bool
	Pending           int
	Processing        int
	Dead              int
	OldestPending     time.Duration
	OldestPendingSeen bool
}

type hostOutboxSnapshotFunc func(context.Context) (hostOutboxSnapshot, error)

func newHostMetrics() *hostMetrics {
	return &hostMetrics{events: make(map[string]uint64)}
}

func (m *hostMetrics) record(event string) {
	if m == nil {
		return
	}
	event = normalizeHostEvent(event)
	m.mu.Lock()
	m.events[event]++
	m.mu.Unlock()
}

func (m *hostMetrics) recordOutboxError() {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.outboxErrors++
	m.mu.Unlock()
}

func normalizeHostEvent(event string) string {
	switch {
	case strings.HasPrefix(event, "reconciliation_waiting_for_core"):
		return "reconciliation_waiting_for_core"
	case strings.HasPrefix(event, "reconciliation_failed"):
		return "reconciliation_failed"
	}
	switch event {
	case "host_probe_started", "host_worker_started", "host_workers_started", "host_stopped":
		return event
	case "reconciliation_recovered":
		return event
	case "projection_failed", "parser_attachments_imported", "review_scan_failed",
		"attachment_cleanup_failed", "attachment_cleanup_retry", "dependency_refresh_failed",
		"lifecycle_scan_failed":
		return event
	default:
		return "other"
	}
}

func newHostOutboxSnapshot(store *outbox.Store, scopes []outbox.Scope, now func() time.Time) hostOutboxSnapshotFunc {
	if now == nil {
		now = time.Now
	}
	return func(ctx context.Context) (hostOutboxSnapshot, error) {
		if store == nil {
			return hostOutboxSnapshot{}, nil
		}
		snapshot := hostOutboxSnapshot{Available: true}
		for _, scope := range scopes {
			stats, err := store.Stats(ctx, scope, now())
			if err != nil {
				return hostOutboxSnapshot{}, err
			}
			snapshot.Pending += stats.Pending
			snapshot.Processing += stats.Processing
			snapshot.Dead += stats.Dead
			if stats.Pending+stats.Processing > 0 {
				if !snapshot.OldestPendingSeen || stats.OldestPendingAge > snapshot.OldestPending {
					snapshot.OldestPending = stats.OldestPendingAge
					snapshot.OldestPendingSeen = true
				}
			}
		}
		return snapshot, nil
	}
}

// handler exposes host event and outbox aggregates on the loopback Bridge.
func (m *hostMetrics) handler(snapshot hostOutboxSnapshotFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var state hostOutboxSnapshot
		if snapshot != nil {
			var err error
			state, err = snapshot(r.Context())
			if err != nil {
				m.recordOutboxError()
				state = hostOutboxSnapshot{}
			}
		}

		m.mu.Lock()
		events := make(map[string]uint64, len(m.events))
		for event, count := range m.events {
			events[event] = count
		}
		outboxErrors := m.outboxErrors
		m.mu.Unlock()

		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		fmt.Fprintln(w, "# HELP host_reconciliation_events_total Fixed Host event counts.")
		fmt.Fprintln(w, "# TYPE host_reconciliation_events_total counter")
		eventKeys := make([]string, 0, len(events))
		for event := range events {
			eventKeys = append(eventKeys, event)
		}
		sort.Strings(eventKeys)
		for _, event := range eventKeys {
			fmt.Fprintf(w, "host_reconciliation_events_total{event=%s} %d\n",
				quoteHostLabel(event), events[event])
		}

		fmt.Fprintln(w, "# HELP host_outbox_available Whether the Host outbox snapshot is available.")
		fmt.Fprintln(w, "# TYPE host_outbox_available gauge")
		fmt.Fprintln(w, "# HELP host_outbox_query_errors_total Outbox aggregate query failures.")
		fmt.Fprintln(w, "# TYPE host_outbox_query_errors_total counter")
		fmt.Fprintf(w, "host_outbox_query_errors_total %d\n", outboxErrors)

		if !state.Available {
			fmt.Fprintln(w, "host_outbox_available 0")
			return
		}
		fmt.Fprintln(w, "host_outbox_available 1")
		fmt.Fprintln(w, "# HELP host_outbox_items Current Host outbox items by fixed status.")
		fmt.Fprintln(w, "# TYPE host_outbox_items gauge")
		fmt.Fprintf(w, "host_outbox_items{status=\"pending\"} %d\n", state.Pending)
		fmt.Fprintf(w, "host_outbox_items{status=\"processing\"} %d\n", state.Processing)
		fmt.Fprintf(w, "host_outbox_items{status=\"dead\"} %d\n", state.Dead)
		fmt.Fprintln(w, "# HELP host_outbox_oldest_pending_available Whether an oldest pending item was observed.")
		fmt.Fprintln(w, "# TYPE host_outbox_oldest_pending_available gauge")
		if state.OldestPendingSeen {
			fmt.Fprintln(w, "host_outbox_oldest_pending_available 1")
			fmt.Fprintln(w, "# HELP host_outbox_oldest_pending_seconds Age of the oldest pending/processing item.")
			fmt.Fprintln(w, "# TYPE host_outbox_oldest_pending_seconds gauge")
			fmt.Fprintf(w, "host_outbox_oldest_pending_seconds %s\n",
				strconv.FormatFloat(state.OldestPending.Seconds(), 'f', -1, 64))
		} else {
			fmt.Fprintln(w, "host_outbox_oldest_pending_available 0")
		}
	})
}

func quoteHostLabel(value string) string {
	value = strings.ReplaceAll(value, "\\", "\\\\")
	value = strings.ReplaceAll(value, "\"", "\\\"")
	value = strings.ReplaceAll(value, "\n", "\\n")
	return "\"" + value + "\""
}
