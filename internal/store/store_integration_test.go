//go:build integration

package store

import (
	"context"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"reconpilot/internal/domain"
	"reconpilot/internal/engine"
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

// A statement that fails inside SaveBatch must roll back the whole file,
// including the batch row and the rows inserted before it.
func TestSaveBatchKeepsNothingOnFailure(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	bad := mkTx("TX-2", 500)
	bad.Direction = "sideways" // breaks the direction CHECK constraint
	if _, _, err := s.SaveBatch(ctx, domain.SourcePSP, "psp.csv", []domain.Transaction{mkTx("TX-1", 1000), bad}, nil); err == nil {
		t.Fatal("accepted a transaction that breaks the schema")
	}
	var batches, txs int
	if err := s.pool.QueryRow(ctx,
		`SELECT (SELECT count(*) FROM source_batch), (SELECT count(*) FROM transaction)`).Scan(&batches, &txs); err != nil {
		t.Fatal(err)
	}
	if batches != 0 || txs != 0 {
		t.Fatalf("a failed batch left %d batch rows and %d transactions", batches, txs)
	}
}

// A database created before migration 0002 refuses the split and reversal
// kinds and the ambiguous type. Migrate must widen both constraints once and
// leave them alone on the next start, and a run that produces all three must
// then persist.
func TestMigrationAcceptsSplitReversalAndAmbiguous(t *testing.T) {
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
	initSQL, err := migrationsFS.ReadFile("migrations/0001_init.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, string(initSQL)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, `INSERT INTO match_group (kind) VALUES ('split')`); err == nil {
		t.Fatal("the initial schema accepted kind 'split'; the test is not exercising an old database")
	}
	constraintOIDs := func() (kind, typ uint32) {
		if err := s.pool.QueryRow(ctx, `SELECT
			(SELECT oid FROM pg_constraint WHERE conname = 'match_group_kind_check'),
			(SELECT oid FROM pg_constraint WHERE conname = 'discrepancy_type_check')`).Scan(&kind, &typ); err != nil {
			t.Fatal(err)
		}
		return kind, typ
	}
	if err := s.Migrate(ctx); err != nil {
		t.Fatalf("first migrate: %v", err)
	}
	kind1, typ1 := constraintOIDs()
	if err := s.Migrate(ctx); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
	// A rewritten constraint gets a new oid; the second start must leave
	// both alone.
	if kind2, typ2 := constraintOIDs(); kind2 != kind1 || typ2 != typ1 {
		t.Fatalf("second migrate rewrote a constraint: oids %d,%d then %d,%d", kind1, typ1, kind2, typ2)
	}

	at := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	row := func(src domain.SourceType, ref string, amount int64, dir domain.Direction, n int) domain.Transaction {
		key := domain.DedupKey(src, ref, amount, at, dir)
		if n > 1 {
			key = domain.OccurrenceKey(key, n)
		}
		return domain.Transaction{Source: src, ExternalRef: ref, CounterpartyRef: "seller-1", AmountKurus: amount,
			Currency: "TRY", OccurredAt: at, Direction: dir, DedupKey: key, Status: domain.StatusUnmatched}
	}
	psp := []domain.Transaction{
		row(domain.SourcePSP, "TX-5", 10000, domain.DirCredit, 1),
		row(domain.SourcePSP, "TX-9", 5000, domain.DirCredit, 1),
		row(domain.SourcePSP, "TX-9", 5000, domain.DirDebit, 1),
		row(domain.SourcePSP, "TX-1", 40000, domain.DirCredit, 1),
		row(domain.SourcePSP, "TX-2", 40000, domain.DirCredit, 1),
	}
	bank := []domain.Transaction{
		row(domain.SourceBank, "TX-5", 6000, domain.DirCredit, 1),
		row(domain.SourceBank, "TX-5", 4000, domain.DirCredit, 1),
		row(domain.SourceBank, "", 40000, domain.DirCredit, 1),
		row(domain.SourceBank, "", 40000, domain.DirCredit, 2),
	}
	if _, _, err := s.SaveBatch(ctx, domain.SourcePSP, "psp.csv", psp, nil); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.SaveBatch(ctx, domain.SourceBank, "bank.csv", bank, nil); err != nil {
		t.Fatal(err)
	}
	all, err := s.LoadTransactions(ctx)
	if err != nil || len(all) != 9 {
		t.Fatalf("want 9 stored rows, got %d err=%v", len(all), err)
	}
	out, err := engine.Run(all)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SaveResult(ctx, out.Matches, out.Discrepancies); err != nil {
		t.Fatalf("saving a run with the new kinds and type: %v", err)
	}
	var splits, reversals, ambiguous int
	if err := s.pool.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM match_group WHERE kind='split'),
		(SELECT count(*) FROM match_group WHERE kind='reversal'),
		(SELECT count(*) FROM discrepancy WHERE type='ambiguous')`).Scan(&splits, &reversals, &ambiguous); err != nil {
		t.Fatal(err)
	}
	if splits != 1 || reversals != 1 || ambiguous != 4 {
		t.Fatalf("stored %d split, %d reversal, %d ambiguous; want 1, 1, 4", splits, reversals, ambiguous)
	}
}
