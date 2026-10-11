package ingestion

import (
	"strings"
	"testing"

	"reconpilot/internal/domain"
)

func TestAdaptersRejectUnsupportedMoney(t *testing.T) {
	for _, src := range []domain.SourceType{domain.SourcePSP, domain.SourceBank} {
		header := "ref,counterparty,amount_kurus,currency,occurred_at,direction\n"
		if src == domain.SourceBank {
			header = "stmt_ref,counterparty,amount_kurus,currency,value_date,direction\n"
		}
		a, err := AdapterFor(src)
		if err != nil {
			t.Fatal(err)
		}
		txs, rejected, err := a.Parse(strings.NewReader(header +
			"a,shop,100,TRY,2026-07-01,credit\n" +
			"a,shop,100,USD,2026-07-01,credit\n" +
			"c,shop,100,,2026-07-01,credit\n" +
			"d,shop,100, TRY,2026-07-01,credit\n" +
			"b,shop,-1,TRY,2026-07-01,credit\n"))
		if err != nil || len(txs) != 1 || len(rejected) != 4 {
			t.Fatalf("%s: accepted=%d rejected=%d err=%v", src, len(txs), len(rejected), err)
		}
		for i, want := range []string{"currency", "currency", "currency", "nonnegative"} {
			if !strings.Contains(rejected[i].Reason, want) {
				t.Errorf("%s: row %d rejected for %q, want a %s error", src, rejected[i].RowNo, rejected[i].Reason, want)
			}
		}
	}
}
