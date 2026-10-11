//go:build integration

package ingestion

// newTestStore copies the store package's test helper, because helpers in
// _test.go files cannot be imported from another package.

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"reconpilot/internal/domain"
	"reconpilot/internal/store"
)

func newTestStore(t *testing.T) (*store.Store, string) {
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
	s, err := store.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	return s, url
}

func queryRow(t *testing.T, url, sql string, args []any, dest ...any) {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	if err := conn.QueryRow(ctx, sql, args...).Scan(dest...); err != nil {
		t.Fatal(err)
	}
}

func TestIngestTwiceAddsNoTransactions(t *testing.T) {
	s, url := newTestStore(t)
	ctx := context.Background()
	first, err := Ingest(ctx, s, domain.SourcePSP, "psp.csv", strings.NewReader(pspCSV))
	if err != nil {
		t.Fatal(err)
	}
	if first.Parsed != 2 || first.Inserted != 2 || first.Rejected != 1 {
		t.Fatalf("first: %+v", first)
	}
	var status string
	var rows, rejected int
	queryRow(t, url, `SELECT status, row_count, rejected_count FROM source_batch WHERE id = $1`,
		[]any{first.BatchID}, &status, &rows, &rejected)
	if status != "ingested" || rows != 2 || rejected != 1 {
		t.Fatalf("batch row: status=%q rows=%d rejected=%d", status, rows, rejected)
	}
	second, err := Ingest(ctx, s, domain.SourcePSP, "psp.csv", strings.NewReader(pspCSV))
	if err != nil {
		t.Fatal(err)
	}
	if second.Inserted != 0 || second.Deduped != 2 {
		t.Fatalf("second ingest added transactions: %+v", second)
	}
}

func TestIngestBadHeaderStoresNothing(t *testing.T) {
	s, url := newTestStore(t)
	if _, err := Ingest(context.Background(), s, domain.SourcePSP, "bad.csv",
		strings.NewReader("wrong,header\nx,y\n")); err == nil {
		t.Fatal("accepted a file with the wrong header")
	}
	var batches int
	queryRow(t, url, `SELECT count(*) FROM source_batch`, nil, &batches)
	if batches != 0 {
		t.Fatalf("a failed ingest left %d batch rows", batches)
	}
}
