package server

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"reconpilot/internal/classification"
	"reconpilot/internal/domain"
	"reconpilot/internal/matching"
	"reconpilot/internal/ports"
)

type fakeStore struct {
	pingErr error
	loadErr error
	saveErr error
	txs     []domain.Transaction
	saved   bool
}

type blockingStore struct {
	fakeStore
	entered chan struct{}
	release chan struct{}
}

func (b *blockingStore) LoadTransactions(ctx context.Context) ([]domain.Transaction, error) {
	select {
	case b.entered <- struct{}{}:
	default:
	}
	select {
	case <-b.release:
		return b.txs, b.loadErr
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (f *fakeStore) Ping(context.Context) error { return f.pingErr }

func (f *fakeStore) LoadTransactions(context.Context) ([]domain.Transaction, error) {
	return f.txs, f.loadErr
}

func (f *fakeStore) SaveResult(context.Context, []matching.Match, []classification.Discrepancy) error {
	if f.saveErr != nil {
		return f.saveErr
	}
	f.saved = true
	return nil
}

func testHandler(s ports.ReconciliationStore) http.Handler {
	return New(s, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestHealthAndReadiness(t *testing.T) {
	t.Run("healthy", func(t *testing.T) {
		h := testHandler(&fakeStore{})

		for _, path := range []string{"/healthz", "/readyz"} {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
			if rec.Code != http.StatusOK {
				t.Fatalf("GET %s status = %d, want 200", path, rec.Code)
			}
			if got := rec.Header().Get("Content-Type"); got != "application/json" {
				t.Fatalf("GET %s content type = %q", path, got)
			}
		}
	})

	t.Run("database unavailable", func(t *testing.T) {
		h := testHandler(&fakeStore{pingErr: errors.New("database unavailable")})
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want 503", rec.Code)
		}
		if strings.Contains(rec.Body.String(), "database unavailable") {
			t.Fatal("readiness response leaked the internal error")
		}
	})
}

func TestReconciliationRunPersistsAndReportsMetrics(t *testing.T) {
	now := time.Date(2026, time.July, 22, 12, 0, 0, 0, time.UTC)
	s := &fakeStore{txs: []domain.Transaction{
		{ID: 1, Source: domain.SourcePSP, ExternalRef: "ORDER-1", CounterpartyRef: "SHOP-1", AmountKurus: 12500, Currency: "TRY", OccurredAt: now, Direction: domain.DirCredit},
		{ID: 2, Source: domain.SourceBank, ExternalRef: "ORDER-1", CounterpartyRef: "SHOP-1", AmountKurus: 12500, Currency: "TRY", OccurredAt: now, Direction: domain.DirCredit},
	}}
	h := testHandler(s)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/reconciliation-runs", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if !s.saved {
		t.Fatal("reconciliation result was not persisted")
	}
	for _, want := range []string{
		`"transactions":2`,
		`"match_groups":1`,
		`"matched_transactions":2`,
		`"discrepancies":0`,
		`"exact":1`,
		`"invariants":"passed"`,
	} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("response %q does not contain %q", rec.Body.String(), want)
		}
	}

	metricsRec := httptest.NewRecorder()
	h.ServeHTTP(metricsRec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if metricsRec.Code != http.StatusOK {
		t.Fatalf("metrics status = %d", metricsRec.Code)
	}
	for _, want := range []string{
		"reconpilot_reconciliation_runs_total 1",
		"reconpilot_reconciliation_failures_total 0",
		"reconpilot_last_reconciliation_transactions 2",
		"reconpilot_last_reconciliation_match_groups 1",
	} {
		if !strings.Contains(metricsRec.Body.String(), want) {
			t.Errorf("metrics do not contain %q\n%s", want, metricsRec.Body.String())
		}
	}
}

func TestReconciliationFailureIsCountedAndSanitized(t *testing.T) {
	s := &fakeStore{loadErr: errors.New("password=do-not-leak")}
	h := testHandler(s)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/reconciliation-runs", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "do-not-leak") {
		t.Fatal("API response leaked the internal error")
	}

	metricsRec := httptest.NewRecorder()
	h.ServeHTTP(metricsRec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if !strings.Contains(metricsRec.Body.String(), "reconpilot_reconciliation_failures_total 1") {
		t.Fatalf("failed run was not counted\n%s", metricsRec.Body.String())
	}
}

func TestConcurrentReconciliationIsRejected(t *testing.T) {
	s := &blockingStore{entered: make(chan struct{}, 1), release: make(chan struct{})}
	h := testHandler(s)
	firstDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/reconciliation-runs", nil))
		firstDone <- rec
	}()

	select {
	case <-s.entered:
	case <-time.After(time.Second):
		t.Fatal("first reconciliation did not start")
	}

	second := httptest.NewRecorder()
	h.ServeHTTP(second, httptest.NewRequest(http.MethodPost, "/api/v1/reconciliation-runs", nil))
	if second.Code != http.StatusConflict {
		t.Fatalf("concurrent status = %d, want 409", second.Code)
	}
	close(s.release)
	select {
	case first := <-firstDone:
		if first.Code != http.StatusOK {
			t.Fatalf("first status = %d, want 200", first.Code)
		}
	case <-time.After(time.Second):
		t.Fatal("first reconciliation did not finish")
	}
}

func TestReportAndRootRedirect(t *testing.T) {
	h := testHandler(&fakeStore{})

	rootRec := httptest.NewRecorder()
	h.ServeHTTP(rootRec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rootRec.Code != http.StatusTemporaryRedirect || rootRec.Header().Get("Location") != "/report" {
		t.Fatalf("root response = %d location=%q", rootRec.Code, rootRec.Header().Get("Location"))
	}

	notFoundRec := httptest.NewRecorder()
	h.ServeHTTP(notFoundRec, httptest.NewRequest(http.MethodGet, "/does-not-exist", nil))
	if notFoundRec.Code != http.StatusNotFound {
		t.Fatalf("unknown route status = %d, want 404", notFoundRec.Code)
	}

	reportRec := httptest.NewRecorder()
	h.ServeHTTP(reportRec, httptest.NewRequest(http.MethodGet, "/report", nil))
	if reportRec.Code != http.StatusOK {
		t.Fatalf("report status = %d; body=%s", reportRec.Code, reportRec.Body.String())
	}
	if got := reportRec.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
		t.Fatalf("report content type = %q", got)
	}
	if !strings.Contains(reportRec.Body.String(), "ReconPilot") {
		t.Fatal("report body does not contain the page title")
	}
}
