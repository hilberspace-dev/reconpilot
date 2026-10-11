package server

import (
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
)

type routeMetric struct {
	name  string
	count atomic.Uint64
}

type metrics struct {
	routes            []routeMetric
	runs              atomic.Uint64
	failures          atomic.Uint64
	databaseReady     atomic.Int64
	lastDurationNS    atomic.Int64
	lastTransactions  atomic.Int64
	lastMatchGroups   atomic.Int64
	lastDiscrepancies atomic.Int64
}

func newMetrics() *metrics {
	names := []string{
		"/",
		"/api/v1/reconciliation-runs",
		"/healthz",
		"/metrics",
		"/readyz",
		"/report",
	}
	m := &metrics{routes: make([]routeMetric, len(names))}
	for i, name := range names {
		m.routes[i].name = name
	}
	return m
}

func (m *metrics) instrument(route string, next http.Handler) http.Handler {
	var counter *atomic.Uint64
	for i := range m.routes {
		if m.routes[i].name == route {
			counter = &m.routes[i].count
			break
		}
	}
	if counter == nil {
		panic("unregistered metrics route: " + route)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		counter.Add(1)
		next.ServeHTTP(w, r)
	})
}

func (m *metrics) recordSuccess(summary runSummary, duration time.Duration) {
	m.lastDurationNS.Store(duration.Nanoseconds())
	m.lastTransactions.Store(int64(summary.Transactions))
	m.lastMatchGroups.Store(int64(summary.MatchGroups))
	m.lastDiscrepancies.Store(int64(summary.Discrepancies))
}

func (m *metrics) serveHTTP(w http.ResponseWriter, _ *http.Request) {
	var b strings.Builder
	b.WriteString("# HELP reconpilot_http_requests_total HTTP requests received by stable route.\n")
	b.WriteString("# TYPE reconpilot_http_requests_total counter\n")
	for i := range m.routes {
		fmt.Fprintf(&b, "reconpilot_http_requests_total{route=%q} %d\n",
			m.routes[i].name, m.routes[i].count.Load())
	}
	b.WriteString("# HELP reconpilot_reconciliation_runs_total Reconciliation run attempts.\n")
	b.WriteString("# TYPE reconpilot_reconciliation_runs_total counter\n")
	fmt.Fprintf(&b, "reconpilot_reconciliation_runs_total %d\n", m.runs.Load())
	b.WriteString("# HELP reconpilot_reconciliation_failures_total Failed reconciliation runs.\n")
	b.WriteString("# TYPE reconpilot_reconciliation_failures_total counter\n")
	fmt.Fprintf(&b, "reconpilot_reconciliation_failures_total %d\n", m.failures.Load())
	b.WriteString("# HELP reconpilot_database_ready Whether the latest readiness check reached PostgreSQL.\n")
	b.WriteString("# TYPE reconpilot_database_ready gauge\n")
	fmt.Fprintf(&b, "reconpilot_database_ready %d\n", m.databaseReady.Load())
	b.WriteString("# HELP reconpilot_last_reconciliation_duration_seconds Duration of the latest successful run.\n")
	b.WriteString("# TYPE reconpilot_last_reconciliation_duration_seconds gauge\n")
	fmt.Fprintf(&b, "reconpilot_last_reconciliation_duration_seconds %.6f\n",
		float64(m.lastDurationNS.Load())/float64(time.Second))
	b.WriteString("# HELP reconpilot_last_reconciliation_transactions Transactions in the latest successful run.\n")
	b.WriteString("# TYPE reconpilot_last_reconciliation_transactions gauge\n")
	fmt.Fprintf(&b, "reconpilot_last_reconciliation_transactions %d\n", m.lastTransactions.Load())
	b.WriteString("# HELP reconpilot_last_reconciliation_match_groups Match groups in the latest successful run.\n")
	b.WriteString("# TYPE reconpilot_last_reconciliation_match_groups gauge\n")
	fmt.Fprintf(&b, "reconpilot_last_reconciliation_match_groups %d\n", m.lastMatchGroups.Load())
	b.WriteString("# HELP reconpilot_last_reconciliation_discrepancies Discrepancies in the latest successful run.\n")
	b.WriteString("# TYPE reconpilot_last_reconciliation_discrepancies gauge\n")
	fmt.Fprintf(&b, "reconpilot_last_reconciliation_discrepancies %d\n", m.lastDiscrepancies.Load())

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(b.String()))
}
