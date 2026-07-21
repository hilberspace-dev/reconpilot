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
