package ingestion

import (
	"strings"
	"testing"

	"reconpilot/internal/domain"
)

const pspCSV = `ref,counterparty,amount_kurus,currency,occurred_at,direction
TX-1,seller-1,10050,TRY,2026-07-01,credit
TX-2,seller-1,notanumber,TRY,2026-07-01,credit
TX-3,seller-2,20000,TRY,2026-07-02,credit
`

func TestPSPAdapterParsesAndRejects(t *testing.T) {
	a, err := AdapterFor(domain.SourcePSP)
	if err != nil {
		t.Fatal(err)
	}
	txs, rejected, err := a.Parse(strings.NewReader(pspCSV))
	if err != nil {
		t.Fatal(err)
	}
	if len(txs) != 2 {
		t.Fatalf("want 2 parsed, got %d", len(txs))
	}
	if len(rejected) != 1 || rejected[0].RowNo != 3 {
		t.Fatalf("want row 3 rejected, got %+v", rejected)
	}
	tx := txs[0]
	if tx.ExternalRef != "TX-1" || tx.AmountKurus != 10050 || tx.CounterpartyRef != "seller-1" {
		t.Fatalf("bad parse: %+v", tx)
	}
	if tx.DedupKey == "" || tx.Status != domain.StatusUnmatched || tx.Source != domain.SourcePSP {
		t.Fatalf("metadata not set: %+v", tx)
	}
}
