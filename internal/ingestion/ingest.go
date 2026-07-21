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
	batchID, err := s.CreateBatch(ctx, src, fileRef)
	if err != nil {
		return Summary{}, err
	}
	txs, rejected, err := adapter.Parse(r)
	if err != nil {
		return Summary{}, err
	}
	inserted, err := s.UpsertTransactions(ctx, batchID, txs)
	if err != nil {
		return Summary{}, err
	}
	for _, rr := range rejected {
		if err := s.InsertRejectedRow(ctx, batchID, rr.RowNo, rr.Reason, rr.Raw); err != nil {
			return Summary{}, err
		}
	}
	return Summary{
		BatchID: batchID, Parsed: len(txs), Inserted: inserted,
		Deduped: len(txs) - inserted, Rejected: len(rejected),
	}, nil
}
