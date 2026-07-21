package matching

import (
	"testing"

	"reconpilot/internal/domain"
)

func TestMatchTolerantWithinWindows(t *testing.T) {
	p := SplitPool([]domain.Transaction{
		tx(1, domain.SourcePSP, "TX-1", 100000, 1),
		tx(2, domain.SourceBank, "STMT-9", 99950, 3), // -0.05%, +2 days, same counterparty → tolerant
	})
	matches := MatchTolerant(&p)
	if len(matches) != 1 || matches[0].Kind != "tolerant" {
		t.Fatalf("want tolerant match, got %+v", matches)
	}
	if matches[0].ScoreBP >= 10000 || matches[0].ScoreBP <= 0 {
		t.Fatalf("score must be in (0,10000): %d", matches[0].ScoreBP)
	}
}

func TestMatchTolerantRespectsBounds(t *testing.T) {
	// 1% off — outside the 0.5% window.
	p := SplitPool([]domain.Transaction{
		tx(1, domain.SourcePSP, "TX-1", 100000, 1),
		tx(2, domain.SourceBank, "STMT-9", 99000, 2),
	})
	if got := MatchTolerant(&p); len(got) != 0 {
		t.Fatalf("1%% diff must not match: %+v", got)
	}
	// 4 days late — outside the 3-day window.
	p2 := SplitPool([]domain.Transaction{
		tx(1, domain.SourcePSP, "TX-1", 100000, 1),
		tx(2, domain.SourceBank, "STMT-9", 100000, 5),
	})
	if got := MatchTolerant(&p2); len(got) != 0 {
		t.Fatalf("4-day shift must not match: %+v", got)
	}
}

func TestMatchTolerantIgnoresMarketplacePayouts(t *testing.T) {
	// A payout whose net lands within 0.5% of an open order must NOT be
	// tolerant-matched — payouts reconcile via the group matcher only.
	mp := tx(2, domain.SourceMarketplace, "PAY-1", 99950, 2)
	mp.GrossKurus, mp.CommissionKurus = 117000, 17050
	p := SplitPool([]domain.Transaction{
		tx(1, domain.SourcePSP, "TX-1", 100000, 1),
		mp,
	})
	if got := MatchTolerant(&p); len(got) != 0 {
		t.Fatalf("marketplace payout must not tolerant-match: %+v", got)
	}
}
