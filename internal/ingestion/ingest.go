package ingestion

import (
	"context"
	"io"

	"reconpilot/internal/domain"
	"reconpilot/internal/store"
)

type Summary struct {
	BatchID  int64
	Parsed   int
	Inserted int
	Deduped  int
	Rejected int
}

func Ingest(ctx context.Context, s *store.Store, src domain.SourceType, fileRef string, r io.Reader) (Summary, error) {
	adapter, err := AdapterFor(src)
	if err != nil {
		return Summary{}, err
	}
	// Parse before touching the database, so a file with a bad header
	// leaves no batch behind.
	txs, rejected, err := adapter.Parse(r)
	if err != nil {
		return Summary{}, err
	}
	rows := make([]store.RejectedRow, len(rejected))
	for i, rr := range rejected {
		rows[i] = store.RejectedRow{RowNo: rr.RowNo, Reason: rr.Reason, Raw: rr.Raw}
	}
	batchID, inserted, err := s.SaveBatch(ctx, src, fileRef, txs, rows)
	if err != nil {
		return Summary{}, err
	}
	return Summary{
		BatchID: batchID, Parsed: len(txs), Inserted: inserted,
		Deduped: len(txs) - inserted, Rejected: len(rejected),
	}, nil
}
