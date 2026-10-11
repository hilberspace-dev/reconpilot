package ingestion

import (
	"strings"
	"testing"

	"reconpilot/internal/domain"
)

const mpCSV = `payout_ref,counterparty,gross_kurus,commission_kurus,net_kurus,payout_date
PAY-1,seller-1,100000,15000,85000,2026-07-05
PAY-2,seller-1,100000,15000,90000,2026-07-05
`

func TestMarketplaceAdapterEnforcesBreakdown(t *testing.T) {
	a, _ := AdapterFor(domain.SourceMarketplace)
	txs, rejected, err := a.Parse(strings.NewReader(mpCSV))
	if err != nil {
		t.Fatal(err)
	}
	if len(txs) != 1 || len(rejected) != 1 {
		t.Fatalf("PAY-2 must be rejected (gross-commission != net): txs=%d rejected=%d", len(txs), len(rejected))
	}
	tx := txs[0]
	if tx.AmountKurus != 85000 || tx.GrossKurus != 100000 || tx.CommissionKurus != 15000 {
		t.Fatalf("bad breakdown: %+v", tx)
	}
}
