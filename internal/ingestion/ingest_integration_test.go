package ingestion

// NOTE: this test needs the same testcontainers bootstrap as the store tests.
// To avoid an import cycle (store tests can't import ingestion helpers),
// duplicate the small newTestStore helper here.

import (
	"context"
	"strings"
	"testing"

	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"reconpilot/internal/domain"
	"reconpilot/internal/store"
)

func newTestStore(t *testing.T) *store.Store {
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
	return s
}

func TestIngestTwiceIsNoOp(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	first, err := Ingest(ctx, s, domain.SourcePSP, "psp.csv", strings.NewReader(pspCSV))
	if err != nil {
		t.Fatal(err)
	}
	if first.Parsed != 2 || first.Inserted != 2 || first.Rejected != 1 {
		t.Fatalf("first: %+v", first)
	}
	second, err := Ingest(ctx, s, domain.SourcePSP, "psp.csv", strings.NewReader(pspCSV))
	if err != nil {
		t.Fatal(err)
	}
	if second.Inserted != 0 || second.Deduped != 2 {
		t.Fatalf("second ingest must be a no-op: %+v", second)
	}
}
