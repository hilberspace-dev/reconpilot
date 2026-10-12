package ingestion

import (
	"bytes"
	"testing"

	"reconpilot/internal/domain"
)

// Whatever bytes arrive, every adapter either refuses the file or returns
// rows that obey the money rules, with a dedup key each, distinct within the
// file.
func fuzzAdapter(f *testing.F, src domain.SourceType, seeds ...string) {
	for _, s := range seeds {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		a, _ := AdapterFor(src)
		txs, _, err := a.Parse(bytes.NewReader(data))
		if err != nil {
			return
		}
		keys := map[string]bool{}
		for _, tx := range txs {
			if err := tx.ValidateMoney(); err != nil {
				t.Fatalf("accepted a row that breaks the money rules: %+v: %v", tx, err)
			}
			if tx.DedupKey == "" || keys[tx.DedupKey] {
				t.Fatalf("missing or repeated dedup key: %+v", tx)
			}
			keys[tx.DedupKey] = true
		}
	})
}

func FuzzBankAdapter(f *testing.F) {
	fuzzAdapter(f, domain.SourceBank,
		"stmt_ref,counterparty,amount_kurus,currency,value_date,direction\nTX-1,s1,100,TRY,2026-07-02,credit\n",
		"stmt_ref,counterparty,amount_kurus,currency,value_date,direction\n,s1,100,TRY,2026-07-02,credit\n,s1,100,TRY,2026-07-02,credit\n")
}

func FuzzPSPAdapter(f *testing.F) {
	fuzzAdapter(f, domain.SourcePSP,
		"ref,counterparty,amount_kurus,currency,occurred_at,direction\nTX-1,s1,100,TRY,2026-07-01,credit\n")
}

func FuzzMarketplaceAdapter(f *testing.F) {
	fuzzAdapter(f, domain.SourceMarketplace,
		"payout_ref,counterparty,gross_kurus,commission_kurus,net_kurus,payout_date\nPAY-1,s1,1000,150,850,2026-07-05\n")
}
