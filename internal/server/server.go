// Package server exposes ReconPilot's deterministic engine as a small HTTP
// service. The HTTP layer owns no reconciliation rules; it loads records,
// invokes engine.Run, persists the result, and renders stable response shapes.
package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"reconpilot/internal/classification"
	"reconpilot/internal/domain"
	"reconpilot/internal/engine"
	"reconpilot/internal/matching"
	"reconpilot/internal/reporting"
)

// Store is the persistence boundary required by the HTTP service.
type Store interface {
	Ping(context.Context) error
	LoadTransactions(context.Context) ([]domain.Transaction, error)
	SaveResult(context.Context, []matching.Match, []classification.Discrepancy) error
}

type handler struct {
	store   Store
	logger  *slog.Logger
	metrics *metrics
	runMu   sync.Mutex
}

// New returns the complete HTTP surface. Method-aware ServeMux patterns make
// unsupported methods return 405 without custom routing code.
func New(store Store, logger *slog.Logger) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	h := &handler{store: store, logger: logger, metrics: newMetrics()}
	mux := http.NewServeMux()
	mux.Handle("GET /", h.metrics.instrument("/", http.HandlerFunc(h.redirectRoot)))
	mux.Handle("GET /healthz", h.metrics.instrument("/healthz", http.HandlerFunc(h.health)))
	mux.Handle("GET /readyz", h.metrics.instrument("/readyz", http.HandlerFunc(h.ready)))
	mux.Handle("GET /metrics", h.metrics.instrument("/metrics", http.HandlerFunc(h.metrics.serveHTTP)))
	mux.Handle("GET /report", h.metrics.instrument("/report", http.HandlerFunc(h.report)))
	mux.Handle("POST /api/v1/reconciliation-runs", h.metrics.instrument(
		"/api/v1/reconciliation-runs", http.HandlerFunc(h.reconcile)))
	return securityHeaders(mux)
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		next.ServeHTTP(w, r)
	})
}

func (h *handler) redirectRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	http.Redirect(w, r, "/report", http.StatusTemporaryRedirect)
}

func (h *handler) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *handler) ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := h.store.Ping(ctx); err != nil {
		h.metrics.databaseReady.Store(0)
		h.logger.WarnContext(r.Context(), "readiness check failed", "error", err)
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unavailable"})
		return
	}
	h.metrics.databaseReady.Store(1)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func (h *handler) reconcile(w http.ResponseWriter, r *http.Request) {
	if !h.runMu.TryLock() {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "reconciliation already running"})
		return
	}
	defer h.runMu.Unlock()

	h.metrics.runs.Add(1)
	started := time.Now()

	all, err := h.store.LoadTransactions(r.Context())
	if err != nil {
		h.runError(w, r, "load transactions", err)
		return
	}
	out, err := engine.Run(all)
	if err != nil {
		h.runError(w, r, "run reconciliation", err)
		return
	}
	if err := h.store.SaveResult(r.Context(), out.Matches, out.Discrepancies); err != nil {
		h.runError(w, r, "persist reconciliation", err)
		return
	}

	summary := summarize(out)
	h.metrics.recordSuccess(summary, time.Since(started))
	writeJSON(w, http.StatusOK, summary)
}

func (h *handler) runError(w http.ResponseWriter, r *http.Request, operation string, err error) {
	h.metrics.failures.Add(1)
	h.logger.ErrorContext(r.Context(), operation+" failed", "error", err)
	writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "reconciliation failed"})
}

func (h *handler) report(w http.ResponseWriter, r *http.Request) {
	all, err := h.store.LoadTransactions(r.Context())
	if err != nil {
		h.reportError(w, r, "load transactions", err)
		return
	}
	out, err := engine.Run(all)
	if err != nil {
		h.reportError(w, r, "run reconciliation", err)
		return
	}
	htmlBytes, _, err := reporting.Render(out, all)
	if err != nil {
		h.reportError(w, r, "render report", err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(htmlBytes)
}

func (h *handler) reportError(w http.ResponseWriter, r *http.Request, operation string, err error) {
	h.logger.ErrorContext(r.Context(), operation+" failed", "error", err)
	writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "report unavailable"})
}

type runSummary struct {
	Transactions        int               `json:"transactions"`
	MatchGroups         int               `json:"match_groups"`
	MatchedTransactions int               `json:"matched_transactions"`
	Discrepancies       int               `json:"discrepancies"`
	Matches             matchCounts       `json:"matches"`
	DiscrepancyTypes    discrepancyCounts `json:"discrepancy_types"`
	Invariants          string            `json:"invariants"`
}

type matchCounts struct {
	Exact    int `json:"exact"`
	Tolerant int `json:"tolerant"`
	Group    int `json:"group"`
}

type discrepancyCounts struct {
	Commission int `json:"commission"`
	Refund     int `json:"refund"`
	Partial    int `json:"partial"`
	Timing     int `json:"timing"`
	Duplicate  int `json:"duplicate"`
	Missing    int `json:"missing"`
	Unknown    int `json:"unknown"`
}

func summarize(out engine.Output) runSummary {
	s := runSummary{
		Transactions:  out.TotalTx,
		MatchGroups:   len(out.Matches),
		Discrepancies: len(out.Discrepancies),
		Invariants:    "passed",
	}
	for _, match := range out.Matches {
		s.MatchedTransactions += len(match.TxIDs)
		switch match.Kind {
		case "exact":
			s.Matches.Exact++
		case "tolerant":
			s.Matches.Tolerant++
		case "group":
			s.Matches.Group++
		}
	}
	for _, discrepancy := range out.Discrepancies {
		switch discrepancy.Type {
		case "commission":
			s.DiscrepancyTypes.Commission++
		case "refund":
			s.DiscrepancyTypes.Refund++
		case "partial":
			s.DiscrepancyTypes.Partial++
		case "timing":
			s.DiscrepancyTypes.Timing++
		case "duplicate":
			s.DiscrepancyTypes.Duplicate++
		case "missing":
			s.DiscrepancyTypes.Missing++
		case "unknown":
			s.DiscrepancyTypes.Unknown++
		}
	}
	return s
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
