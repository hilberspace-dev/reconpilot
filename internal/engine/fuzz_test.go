package engine_test

import (
	"fmt"
	"testing"
	"time"

	"reconpilot/internal/domain"
	"reconpilot/internal/engine"
)

// Every 6 bytes make one row: source, reference, counterparty, amount, day,
// direction, each drawn from a small set so rows collide. Any book of valid
// rows must run without error, and every row must be placed exactly once.
func FuzzEngineRun(f *testing.F) {
	f.Add([]byte{0, 1, 0, 3, 1, 0, 1, 1, 0, 3, 2, 0, 2, 2, 0, 9, 5, 0})
	f.Fuzz(func(t *testing.T, data []byte) {
		day0 := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
		refs := []string{"", "R1", "R2", "R3"}
		var book []domain.Transaction
		for i := 0; i+6 <= len(data) && len(book) < 60; i += 6 {
			b := data[i : i+6]
			tx := domain.Transaction{ID: int64(len(book) + 1), Currency: "TRY",
				ExternalRef: refs[int(b[1])%len(refs)], CounterpartyRef: fmt.Sprintf("s%d", b[2]%2),
				AmountKurus: int64(b[3]%10+1) * 500, OccurredAt: day0.AddDate(0, 0, int(b[4]%40)),
				Direction: domain.DirCredit}
			if b[5]%4 == 0 {
				tx.Direction = domain.DirDebit
			}
			switch b[0] % 3 {
			case 0:
				tx.Source = domain.SourcePSP
			case 1:
				tx.Source = domain.SourceBank
			default:
				tx.Source, tx.Direction = domain.SourceMarketplace, domain.DirCredit
				tx.ExternalRef = fmt.Sprintf("PAY-%d", tx.ID)
				tx.GrossKurus = tx.AmountKurus * int64(b[5]%3+1)
				tx.CommissionKurus = tx.GrossKurus - tx.AmountKurus
			}
			book = append(book, tx)
		}
		out, err := engine.Run(book)
		if err != nil {
			t.Fatalf("engine refused a valid book: %v", err)
		}
		placed := map[int64]int{}
		for _, m := range out.Matches {
			for _, id := range m.TxIDs {
				placed[id]++
			}
		}
		for _, d := range out.Discrepancies {
			if d.TxID != nil {
				placed[*d.TxID]++
			}
		}
		for _, tx := range book {
			if placed[tx.ID] != 1 {
				t.Fatalf("row %d placed %d times", tx.ID, placed[tx.ID])
			}
		}
	})
}
