// Package metrics provides a deliberately small, process-local metrics
// registry for the P2-E-02 baseline. It is not a replacement for a full
// metrics backend: labels are fixed, values are numeric, and no request body,
// query, tenant, or knowledge-base identifier is accepted.
package metrics

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// SQLiteSlowThreshold is the slow-query boundary shared by the GORM logger and
// the metrics registry.
const SQLiteSlowThreshold = 200 * time.Millisecond

var durationBuckets = []float64{0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60}

type httpKey struct {
	Method      string
	Route       string
	StatusClass string
}

type httpObservation struct {
	Count   uint64
	Buckets []uint64
	Sum     float64
}

type sqliteObservation struct {
	Slow   uint64
	Busy   uint64
	Locked uint64
}

// DBStats is the subset of database/sql.DBStats exposed as gauges. It avoids
// exposing driver-specific state and keeps the metrics package independent of
// the container.
type DBStats struct {
	OpenConnections   int
	InUse             int
	Idle              int
	WaitCount         int64
	WaitDuration      time.Duration
	MaxIdleClosed     int64
	MaxIdleTimeClosed int64
	MaxLifetimeClosed int64
}

// Registry is a concurrency-safe collector for the P2-E-02 P0 metrics.
type Registry struct {
	mu             sync.RWMutex
	http           map[httpKey]*httpObservation
	sqlite         map[string]*sqliteObservation
	dbStats        func() DBStats
	dbStatsPresent bool
}

// New returns an empty registry.
func New() *Registry {
	return &Registry{
		http:   make(map[httpKey]*httpObservation),
		sqlite: make(map[string]*sqliteObservation),
	}
}

// Default is the process-wide registry used by middleware, the GORM logger,
// and the metrics endpoint.
var Default = New()

// Reset clears all observations and the DB stats provider. It exists for
// tests; production code should never need to reset metrics.
func (r *Registry) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.http = make(map[httpKey]*httpObservation)
	r.sqlite = make(map[string]*sqliteObservation)
	r.dbStats = nil
	r.dbStatsPresent = false
}

// SetDBStatsProvider registers a snapshot function for sql.DB pool gauges.
func (r *Registry) SetDBStatsProvider(provider func() DBStats) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.dbStats = provider
	r.dbStatsPresent = provider != nil
}

// ObserveHTTP records one completed HTTP request with a fixed route pattern.
func (r *Registry) ObserveHTTP(method, route string, status int, duration time.Duration) {
	method = normalizeMethod(method)
	route = normalizeRoute(route)
	key := httpKey{Method: method, Route: route, StatusClass: statusClass(status)}
	seconds := duration.Seconds()
	if seconds < 0 {
		seconds = 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	observation := r.http[key]
	if observation == nil {
		observation = &httpObservation{Buckets: make([]uint64, len(durationBuckets))}
		r.http[key] = observation
	}
	observation.Count++
	observation.Sum += seconds
	for i, bucket := range durationBuckets {
		if seconds <= bucket {
			observation.Buckets[i]++
		}
	}
}

// ObserveSQLite records one GORM trace. Slow queries are counted whenever the
// duration reaches SQLiteSlowThreshold; busy/locked counters only increment for
// matching driver errors.
func (r *Registry) ObserveSQLite(operation string, duration time.Duration, err error) {
	operation = normalizeOperation(operation)
	r.mu.Lock()
	defer r.mu.Unlock()
	observation := r.sqlite[operation]
	if observation == nil {
		observation = &sqliteObservation{}
		r.sqlite[operation] = observation
	}
	if duration >= SQLiteSlowThreshold {
		observation.Slow++
	}
	if err == nil {
		return
	}
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "sqlite_busy") || strings.Contains(message, "database is locked") {
		observation.Busy++
	}
	if strings.Contains(message, "sqlite_locked") || strings.Contains(message, "table is locked") {
		observation.Locked++
	}
}

// ServeHTTP renders a Prometheus text exposition for the registry.
func (r *Registry) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")

	r.mu.RLock()
	defer r.mu.RUnlock()

	httpKeys := make([]httpKey, 0, len(r.http))
	for key := range r.http {
		httpKeys = append(httpKeys, key)
	}
	sort.Slice(httpKeys, func(i, j int) bool {
		if httpKeys[i].Method != httpKeys[j].Method {
			return httpKeys[i].Method < httpKeys[j].Method
		}
		if httpKeys[i].Route != httpKeys[j].Route {
			return httpKeys[i].Route < httpKeys[j].Route
		}
		return httpKeys[i].StatusClass < httpKeys[j].StatusClass
	})

	fmt.Fprintln(w, "# HELP core_http_requests_total Total completed Core HTTP requests.")
	fmt.Fprintln(w, "# TYPE core_http_requests_total counter")
	for _, key := range httpKeys {
		value := r.http[key].Count
		fmt.Fprintf(w, "core_http_requests_total{method=%s,route=%s,status_class=%s} %d\n",
			quoteLabel(key.Method), quoteLabel(key.Route), quoteLabel(key.StatusClass), value)
	}

	type durationKey struct {
		Method string
		Route  string
	}
	durations := make(map[durationKey]*httpObservation)
	for key, observation := range r.http {
		aggregateKey := durationKey{Method: key.Method, Route: key.Route}
		aggregate := durations[aggregateKey]
		if aggregate == nil {
			aggregate = &httpObservation{Buckets: make([]uint64, len(durationBuckets))}
			durations[aggregateKey] = aggregate
		}
		aggregate.Count += observation.Count
		aggregate.Sum += observation.Sum
		for i, bucket := range observation.Buckets {
			aggregate.Buckets[i] += bucket
		}
	}
	durationKeys := make([]durationKey, 0, len(durations))
	for key := range durations {
		durationKeys = append(durationKeys, key)
	}
	sort.Slice(durationKeys, func(i, j int) bool {
		if durationKeys[i].Method != durationKeys[j].Method {
			return durationKeys[i].Method < durationKeys[j].Method
		}
		return durationKeys[i].Route < durationKeys[j].Route
	})
	fmt.Fprintln(w, "# HELP core_http_request_duration_seconds Core HTTP request duration in seconds.")
	fmt.Fprintln(w, "# TYPE core_http_request_duration_seconds histogram")
	for _, key := range durationKeys {
		observation := durations[key]
		for i, bucket := range durationBuckets {
			fmt.Fprintf(w, "core_http_request_duration_seconds_bucket{method=%s,route=%s,le=%s} %d\n",
				quoteLabel(key.Method), quoteLabel(key.Route), quoteLabel(formatBucket(bucket)), observation.Buckets[i])
		}
		fmt.Fprintf(w, "core_http_request_duration_seconds_bucket{method=%s,route=%s,le=\"+Inf\"} %d\n",
			quoteLabel(key.Method), quoteLabel(key.Route), observation.Count)
		fmt.Fprintf(w, "core_http_request_duration_seconds_sum{method=%s,route=%s} %s\n",
			quoteLabel(key.Method), quoteLabel(key.Route), formatFloat(observation.Sum))
		fmt.Fprintf(w, "core_http_request_duration_seconds_count{method=%s,route=%s} %d\n",
			quoteLabel(key.Method), quoteLabel(key.Route), observation.Count)
	}

	sqliteKeys := make([]string, 0, len(r.sqlite))
	for operation := range r.sqlite {
		sqliteKeys = append(sqliteKeys, operation)
	}
	sort.Strings(sqliteKeys)
	fmt.Fprintln(w, "# HELP sqlite_slow_queries_total Slow SQLite queries (>= 200ms).")
	fmt.Fprintln(w, "# TYPE sqlite_slow_queries_total counter")
	for _, operation := range sqliteKeys {
		fmt.Fprintf(w, "sqlite_slow_queries_total{operation=%s} %d\n",
			quoteLabel(operation), r.sqlite[operation].Slow)
	}
	fmt.Fprintln(w, "# HELP sqlite_busy_total SQLite SQLITE_BUSY/database-is-locked errors.")
	fmt.Fprintln(w, "# TYPE sqlite_busy_total counter")
	for _, operation := range sqliteKeys {
		fmt.Fprintf(w, "sqlite_busy_total{operation=%s} %d\n",
			quoteLabel(operation), r.sqlite[operation].Busy)
	}
	fmt.Fprintln(w, "# HELP sqlite_locked_total SQLite SQLITE_LOCKED/table-is-locked errors.")
	fmt.Fprintln(w, "# TYPE sqlite_locked_total counter")
	for _, operation := range sqliteKeys {
		fmt.Fprintf(w, "sqlite_locked_total{operation=%s} %d\n",
			quoteLabel(operation), r.sqlite[operation].Locked)
	}

	fmt.Fprintln(w, "# HELP sqlite_conn_stats_available Whether sql.DB pool statistics are available.")
	fmt.Fprintln(w, "# TYPE sqlite_conn_stats_available gauge")
	if !r.dbStatsPresent || r.dbStats == nil {
		fmt.Fprintln(w, "sqlite_conn_stats_available 0")
		return
	}
	fmt.Fprintln(w, "sqlite_conn_stats_available 1")
	stats := r.dbStats()
	fmt.Fprintln(w, "# HELP sqlite_conn_open_total Current open SQL connections.")
	fmt.Fprintln(w, "# TYPE sqlite_conn_open_total gauge")
	fmt.Fprintf(w, "sqlite_conn_open_total %d\n", stats.OpenConnections)
	fmt.Fprintln(w, "# HELP sqlite_conn_in_use Current SQL connections in use.")
	fmt.Fprintln(w, "# TYPE sqlite_conn_in_use gauge")
	fmt.Fprintf(w, "sqlite_conn_in_use %d\n", stats.InUse)
	fmt.Fprintln(w, "# HELP sqlite_conn_idle Current idle SQL connections.")
	fmt.Fprintln(w, "# TYPE sqlite_conn_idle gauge")
	fmt.Fprintf(w, "sqlite_conn_idle %d\n", stats.Idle)
	fmt.Fprintln(w, "# HELP sqlite_conn_wait_total Cumulative SQL connection wait count.")
	fmt.Fprintln(w, "# TYPE sqlite_conn_wait_total counter")
	fmt.Fprintf(w, "sqlite_conn_wait_total %d\n", stats.WaitCount)
	fmt.Fprintln(w, "# HELP sqlite_conn_wait_duration_seconds Cumulative SQL connection wait duration.")
	fmt.Fprintln(w, "# TYPE sqlite_conn_wait_duration_seconds gauge")
	fmt.Fprintf(w, "sqlite_conn_wait_duration_seconds %s\n", formatFloat(stats.WaitDuration.Seconds()))
	fmt.Fprintln(w, "# HELP sqlite_conn_max_idle_closed_total Connections closed because of SetMaxIdleConns.")
	fmt.Fprintln(w, "# TYPE sqlite_conn_max_idle_closed_total counter")
	fmt.Fprintf(w, "sqlite_conn_max_idle_closed_total %d\n", stats.MaxIdleClosed)
	fmt.Fprintln(w, "# HELP sqlite_conn_max_idle_time_closed_total Connections closed because of SetConnMaxIdleTime.")
	fmt.Fprintln(w, "# TYPE sqlite_conn_max_idle_time_closed_total counter")
	fmt.Fprintf(w, "sqlite_conn_max_idle_time_closed_total %d\n", stats.MaxIdleTimeClosed)
	fmt.Fprintln(w, "# HELP sqlite_conn_max_lifetime_closed_total Connections closed because of SetConnMaxLifetime.")
	fmt.Fprintln(w, "# TYPE sqlite_conn_max_lifetime_closed_total counter")
	fmt.Fprintf(w, "sqlite_conn_max_lifetime_closed_total %d\n", stats.MaxLifetimeClosed)
}

func normalizeMethod(method string) string {
	method = strings.ToUpper(strings.TrimSpace(method))
	if method == "" {
		return "OTHER"
	}
	return method
}

func normalizeRoute(route string) string {
	route = strings.TrimSpace(route)
	if route == "" {
		return "unmatched"
	}
	if len(route) > 160 {
		return route[:160]
	}
	return route
}

func statusClass(status int) string {
	if status < 100 || status > 599 {
		return "unknown"
	}
	return strconv.Itoa(status/100) + "xx"
}

func normalizeOperation(operation string) string {
	switch strings.ToLower(strings.TrimSpace(operation)) {
	case "select", "insert", "update", "delete":
		return strings.ToLower(strings.TrimSpace(operation))
	default:
		return "other"
	}
}

func formatBucket(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}

func formatFloat(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}

func quoteLabel(value string) string {
	value = strings.ReplaceAll(value, "\\", "\\\\")
	value = strings.ReplaceAll(value, "\"", "\\\"")
	value = strings.ReplaceAll(value, "\n", "\\n")
	return "\"" + value + "\""
}
