package ingestion

import (
	"strings"
	"testing"

	"reconpilot/internal/domain"
)

const bankCSV = `stmt_ref,counterparty,amount_kurus,currency,value_date,direction
TX-1,seller-1,10050,TRY,2026-07-02,credit
`

func TestBankAdapterParses(t *testing.T) {
	a, _ := AdapterFor(domain.SourceBank)
	txs, rejected, err := a.Parse(strings.NewReader(bankCSV))
	if err != nil || len(txs) != 1 || len(rejected) != 0 {
		t.Fatalf("txs=%d rejected=%d err=%v", len(txs), len(rejected), err)
	}
	if txs[0].Source != domain.SourceBank || txs[0].ExternalRef != "TX-1" {
		t.Fatalf("bad parse: %+v", txs[0])
	}
}

func TestBankAdapterKeepsEqualUnreferencedLines(t *testing.T) {
	// Two customers paid 40.00 on the same day and the bank printed no
	// reference. Both lines are real money; one dedup key for both would let
	// the store drop the second as a re-ingest.
	const csv = `stmt_ref,counterparty,amount_kurus,currency,value_date,direction
,seller-1,4000,TRY,2026-07-02,credit
,seller-2,4000,TRY,2026-07-02,credit
`
	a, _ := AdapterFor(domain.SourceBank)
	first, _, err := a.Parse(strings.NewReader(csv))
	if err != nil || len(first) != 2 {
		t.Fatalf("txs=%d err=%v", len(first), err)
	}
	if first[0].DedupKey == first[1].DedupKey {
		t.Fatal("equal lines in one file must get different dedup keys")
	}
	again, _, _ := a.Parse(strings.NewReader(csv))
	if again[0].DedupKey != first[0].DedupKey || again[1].DedupKey != first[1].DedupKey {
		t.Fatal("re-reading the file must give the same keys")
	}
}
