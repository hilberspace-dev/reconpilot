package store

import (
	"context"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"reconpilot/internal/domain"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	ctx := context.Background()
	pg, err := postgres.Run(ctx, "postgres:17-alpine",
		postgres.WithDatabase("recon"), postgres.WithUsername("recon"), postgres.WithPassword("recon"),
		postgres.BasicWaitStrategies())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pg.Terminate(ctx) })
	url, err := pg.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	s, err := Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	return s
}

func mkTx(ref string, amount int64) domain.Transaction {
	at := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	return domain.Transaction{
		Source: domain.SourcePSP, ExternalRef: ref, CounterpartyRef: "seller-1",
		AmountKurus: amount, Currency: "TRY", OccurredAt: at, Direction: domain.DirCredit,
		DedupKey: domain.DedupKey(domain.SourcePSP, ref, amount, at, domain.DirCredit),
		Status:   domain.StatusUnmatched,
	}
}

// Invariant 4: re-inserting the same rows is a no-op at the schema level.
func TestUpsertIsIdempotent(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	batch, err := s.CreateBatch(ctx, domain.SourcePSP, "psp.csv")
	if err != nil {
		t.Fatal(err)
	}
	txs := []domain.Transaction{mkTx("TX-1", 1000), mkTx("TX-2", 2000)}
	n1, err := s.UpsertTransactions(ctx, batch, txs)
	if err != nil || n1 != 2 {
		t.Fatalf("first insert: n=%d err=%v, want 2,nil", n1, err)
	}
	n2, err := s.UpsertTransactions(ctx, batch, txs)
	if err != nil || n2 != 0 {
		t.Fatalf("re-insert must be no-op: n=%d err=%v, want 0,nil", n2, err)
	}
	all, err := s.LoadTransactions(ctx)
	if err != nil || len(all) != 2 {
		t.Fatalf("want 2 rows after re-ingest, got %d err=%v", len(all), err)
	}
}

// Invariant 2: the DB refuses a transaction in two match groups.
func TestDoubleMatchRejectedBySchema(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	batch, _ := s.CreateBatch(ctx, domain.SourcePSP, "psp.csv")
	if _, err := s.UpsertTransactions(ctx, batch, []domain.Transaction{mkTx("TX-1", 1000)}); err != nil {
		t.Fatal(err)
	}
	all, _ := s.LoadTransactions(ctx)
	txID := all[0].ID
	if err := s.insertRawMatch(ctx, "exact", []int64{txID}); err != nil {
		t.Fatal(err)
	}
	if err := s.insertRawMatch(ctx, "exact", []int64{txID}); err == nil {
		t.Fatal("second membership for same transaction must violate UNIQUE(transaction_id)")
	}
}
