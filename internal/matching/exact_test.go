package matching

import (
	"testing"
	"time"

	"reconpilot/internal/domain"
)

func tx(id int64, src domain.SourceType, ref string, amount int64, day int) domain.Transaction {
	return domain.Transaction{
		ID: id, Source: src, ExternalRef: ref, CounterpartyRef: "seller-1",
		AmountKurus: amount, Currency: "TRY",
		OccurredAt: time.Date(2026, 7, day, 0, 0, 0, 0, time.UTC),
		Direction:  domain.DirCredit, Status: domain.StatusUnmatched,
	}
}

func TestMatchExactPairsByRefAndAmount(t *testing.T) {
	p := SplitPool([]domain.Transaction{
		tx(1, domain.SourcePSP, "TX-1", 1000, 1),
		tx(2, domain.SourcePSP, "TX-2", 2000, 1),
		tx(3, domain.SourceBank, "TX-1", 1000, 2), // ref+amount equal, 1 day → exact
		tx(4, domain.SourceBank, "TX-2", 1999, 2), // amount differs → NOT exact
	})
	matches := MatchExact(&p)
	if len(matches) != 1 {
		t.Fatalf("want 1 exact match, got %d", len(matches))
	}
	m := matches[0]
	if m.Kind != "exact" || m.ScoreBP != 10000 || len(m.TxIDs) != 2 {
		t.Fatalf("bad match: %+v", m)
	}
	if len(p.Expected) != 1 || len(p.Actual) != 1 {
		t.Fatalf("matched txs must leave the pool: expected=%d actual=%d", len(p.Expected), len(p.Actual))
	}
}

func TestMatchExactRespectsDateWindow(t *testing.T) {
	// Same ref+amount but 5 days apart: NOT exact — it must remain in the
	// pool so classification can surface it as a timing discrepancy.
	p := SplitPool([]domain.Transaction{
		tx(1, domain.SourcePSP, "TX-1", 1000, 1),
		tx(2, domain.SourceBank, "TX-1", 1000, 6),
	})
	if got := MatchExact(&p); len(got) != 0 {
		t.Fatalf("5-day shift must not exact-match: %+v", got)
	}
}
