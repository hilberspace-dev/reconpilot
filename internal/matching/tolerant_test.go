package matching

import (
	"reflect"
	"sort"
	"testing"
	"time"
	_ "time/tzdata" // the daylight-saving test needs a real zone on every OS

	"reconpilot/internal/domain"
)

// tolerant runs MatchTolerant and returns the contested rows, sorted.
func tolerant(p *Pool) ([]Match, []int64) {
	matches, rows := MatchTolerant(p)
	sort.Slice(rows, func(i, j int) bool { return rows[i] < rows[j] })
	return matches, rows
}

func TestMatchTolerantSameRefWithinWindows(t *testing.T) {
	p := SplitPool([]domain.Transaction{
		tx(1, domain.SourcePSP, "TX-1", 100000, 1),
		tx(2, domain.SourceBank, "TX-1", 99950, 3), // -0.05%, 2 days later, same reference: tolerant
	})
	matches, ambiguous := tolerant(&p)
	if len(matches) != 1 || matches[0].Kind != "tolerant" || len(ambiguous) != 0 {
		t.Fatalf("want one tolerant match, got %+v, ambiguous %v", matches, ambiguous)
	}
	if matches[0].ScoreBP >= 10000 || matches[0].ScoreBP <= 0 {
		t.Fatalf("score must be in (0,10000): %d", matches[0].ScoreBP)
	}
}

func TestMatchTolerantUnreferencedNeedsTheExactAmount(t *testing.T) {
	// STMT-9 is the bank's own number; nothing on the PSP side uses it.
	p := SplitPool([]domain.Transaction{
		tx(1, domain.SourcePSP, "TX-1", 100000, 1),
		tx(2, domain.SourceBank, "STMT-9", 100000, 3),
	})
	if matches, _ := tolerant(&p); len(matches) != 1 || matches[0].Kind != "tolerant" {
		t.Fatalf("same amount inside the window, only candidate: want a match, got %+v", matches)
	}

	// 0.05% off with no shared reference: a possible partner, not evidence.
	p2 := SplitPool([]domain.Transaction{
		tx(1, domain.SourcePSP, "TX-1", 100000, 1),
		tx(2, domain.SourceBank, "STMT-9", 99950, 3),
	})
	matches, ambiguous := tolerant(&p2)
	if len(matches) != 0 || !reflect.DeepEqual(ambiguous, []int64{1, 2}) {
		t.Fatalf("near amount without a reference must stay open as ambiguous: %+v, ambiguous %v", matches, ambiguous)
	}
}

func TestMatchTolerantRespectsBounds(t *testing.T) {
	// 1% off, outside the 0.5% window.
	p := SplitPool([]domain.Transaction{
		tx(1, domain.SourcePSP, "TX-1", 100000, 1),
		tx(2, domain.SourceBank, "TX-1", 99000, 2),
	})
	if got, amb := tolerant(&p); len(got) != 0 || len(amb) != 0 {
		t.Fatalf("1%% diff must not match: %+v %v", got, amb)
	}
	// 4 days late, outside the 3-day window.
	p2 := SplitPool([]domain.Transaction{
		tx(1, domain.SourcePSP, "TX-1", 100000, 1),
		tx(2, domain.SourceBank, "TX-1", 99950, 5),
	})
	if got, amb := tolerant(&p2); len(got) != 0 || len(amb) != 0 {
		t.Fatalf("4-day shift must not match: %+v %v", got, amb)
	}
}

func TestMatchTolerantIgnoresMarketplacePayouts(t *testing.T) {
	// A payout whose net lands within 0.5% of an open order must not be
	// tolerant-matched; only the group matcher reconciles payouts.
	mp := tx(2, domain.SourceMarketplace, "PAY-1", 99950, 2)
	mp.GrossKurus, mp.CommissionKurus = 117000, 17050
	p := SplitPool([]domain.Transaction{
		tx(1, domain.SourcePSP, "TX-1", 100000, 1),
		mp,
	})
	if got, _ := tolerant(&p); len(got) != 0 {
		t.Fatalf("marketplace payout must not tolerant-match: %+v", got)
	}
}

func TestMatchTolerantDoesNotChooseBetweenEqualCandidates(t *testing.T) {
	// Two orders and two bank lines of one seller, same amount, same day, no
	// reference on the bank side. Either pairing fits; the old greedy pass
	// paired 1 with 3 and 2 with 4.
	p := SplitPool([]domain.Transaction{
		tx(1, domain.SourcePSP, "TX-1", 40000, 5),
		tx(2, domain.SourcePSP, "TX-2", 40000, 5),
		tx(3, domain.SourceBank, "", 40000, 5),
		tx(4, domain.SourceBank, "", 40000, 5),
	})
	matches, ambiguous := tolerant(&p)
	if len(matches) != 0 || !reflect.DeepEqual(ambiguous, []int64{1, 2, 3, 4}) {
		t.Fatalf("want no match and four ambiguous rows, got %+v, ambiguous %v", matches, ambiguous)
	}
}

func TestMatchTolerantRefusesAReferenceThatNamesAnotherOrder(t *testing.T) {
	// The bank line names TX-2, which is on the PSP side, so it settles TX-2
	// (short) and cannot be TX-1's money even though the amount fits.
	p := SplitPool([]domain.Transaction{
		tx(1, domain.SourcePSP, "TX-1", 100000, 1),
		tx(2, domain.SourcePSP, "TX-2", 180000, 1),
		tx(3, domain.SourceBank, "TX-2", 100000, 2),
	})
	if matches, ambiguous := tolerant(&p); len(matches) != 0 || len(ambiguous) != 0 {
		t.Fatalf("contradicting reference must veto the pair: %+v, ambiguous %v", matches, ambiguous)
	}
}

func TestMatchTolerantSameAmountNeedsNoRivalWithinTheHorizon(t *testing.T) {
	// Check 1 was issued on the 1st and its clearance (row 3) came on the
	// 12th. Row 2 is the clearance of an older check of the same vendor and
	// amount, and it lands inside check 1's window. Inside the window row 2
	// is the only fit, but row 3 could just as well be check 1's money.
	p := SplitPool([]domain.Transaction{
		tx(1, domain.SourcePSP, "CHK-1", 70000, 1),
		tx(2, domain.SourceBank, "", 70000, 2),
		tx(3, domain.SourceBank, "", 70000, 12),
	})
	matches, ambiguous := tolerant(&p)
	if len(matches) != 0 || !reflect.DeepEqual(ambiguous, []int64{1, 2}) {
		t.Fatalf("want no match and 1, 2 ambiguous, got %+v, ambiguous %v", matches, ambiguous)
	}

	// With the other clearance 32 days out, beyond the horizon, 1 and 2 match.
	p2 := SplitPool([]domain.Transaction{
		tx(1, domain.SourcePSP, "CHK-1", 70000, 1),
		tx(2, domain.SourceBank, "", 70000, 2),
		tx(3, domain.SourceBank, "", 70000, 33), // 2 August
	})
	matches, ambiguous = tolerant(&p2)
	if len(matches) != 1 || !reflect.DeepEqual(matches[0].TxIDs, []int64{1, 2}) || len(ambiguous) != 0 {
		t.Fatalf("want 1-2 matched, got %+v, ambiguous %v", matches, ambiguous)
	}
}

func TestMatchTolerantReversal(t *testing.T) {
	sale := tx(1, domain.SourcePSP, "TX-9", 5000, 2)
	void := tx(2, domain.SourcePSP, "TX-9", 5000, 2)
	void.Direction = domain.DirDebit
	p := SplitPool([]domain.Transaction{sale, void})
	matches, _ := tolerant(&p)
	if len(matches) != 1 || matches[0].Kind != "reversal" || !reflect.DeepEqual(matches[0].TxIDs, []int64{1, 2}) {
		t.Fatalf("sale and its void under one reference: want a reversal, got %+v", matches)
	}

	// Without references the same pair could be two unrelated entries.
	sale.ExternalRef, void.ExternalRef = "", ""
	p2 := SplitPool([]domain.Transaction{sale, void})
	matches, ambiguous := tolerant(&p2)
	if len(matches) != 0 || len(ambiguous) != 2 {
		t.Fatalf("unreferenced credit and debit must stay open: %+v, ambiguous %v", matches, ambiguous)
	}
}

func TestMatchTolerantSameRefNeedsASinglePartner(t *testing.T) {
	// Both orders carry R and both lie within 0.5 % of the line. The line can
	// only be one order's money, and nothing says which.
	p := SplitPool([]domain.Transaction{
		tx(1, domain.SourcePSP, "R", 10000, 1),
		tx(2, domain.SourcePSP, "R", 9990, 1),
		tx(3, domain.SourceBank, "R", 9995, 1),
	})
	matches, ambiguous := tolerant(&p)
	if len(matches) != 0 || !reflect.DeepEqual(ambiguous, []int64{1, 2, 3}) {
		t.Fatalf("want no match and 1, 2, 3 contested, got %+v, %v", matches, ambiguous)
	}
}

func TestMatchTolerantExpectedRivalWithinTheHorizon(t *testing.T) {
	// Row 2 and line 3 fit inside the window. Row 1, the same amount 22 days
	// before the line, could just as well be the line's source.
	p := SplitPool([]domain.Transaction{
		tx(1, domain.SourcePSP, "X0", 70000, 1),
		tx(2, domain.SourcePSP, "X1", 70000, 22),
		tx(3, domain.SourceBank, "", 70000, 23),
	})
	matches, ambiguous := tolerant(&p)
	if len(matches) != 0 || !reflect.DeepEqual(ambiguous, []int64{2, 3}) {
		t.Fatalf("want no match and 2, 3 contested, got %+v, %v", matches, ambiguous)
	}
}

func TestMatchTolerantOrderSettledElsewhereIsNoRival(t *testing.T) {
	// Order TX-2 has a bank line of its own (row 4, short), so it is not a
	// second candidate source for line 3.
	p := SplitPool([]domain.Transaction{
		tx(1, domain.SourcePSP, "TX-1", 5000, 1),
		tx(2, domain.SourcePSP, "TX-2", 5000, 10),
		tx(3, domain.SourceBank, "", 5000, 2),
		tx(4, domain.SourceBank, "TX-2", 3000, 10),
	})
	matches, _ := tolerant(&p)
	if len(matches) != 1 || !reflect.DeepEqual(matches[0].TxIDs, []int64{1, 3}) {
		t.Fatalf("want 1-3 matched, got %+v", matches)
	}
}

func TestNearCountsWholeDaysAcrossDaylightSaving(t *testing.T) {
	// 95.5 hours is 3 whole days. In New York the clocks moved on 8 March
	// 2026, so a calendar step back from 10 March would cut the row off.
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC).In(ny)
	row := domain.Transaction{OccurredAt: at.Add(-95*time.Hour - 30*time.Minute)}
	if got := near([]domain.Transaction{row}, []int{0}, at, 3); len(got) != 1 {
		t.Fatalf("a row 3 whole days away is outside the window: %v", got)
	}
}

func TestMatchTolerantLineNamingAnotherOrderIsNoRival(t *testing.T) {
	// Line 4 carries TX-2, an order of its own, so it cannot be TX-1's money
	// and does not compete with line 3.
	p := SplitPool([]domain.Transaction{
		tx(1, domain.SourcePSP, "TX-1", 5000, 1),
		tx(2, domain.SourcePSP, "TX-2", 3000, 1),
		tx(3, domain.SourceBank, "", 5000, 2),
		tx(4, domain.SourceBank, "TX-2", 5000, 10),
	})
	matches, _ := tolerant(&p)
	if len(matches) != 1 || !reflect.DeepEqual(matches[0].TxIDs, []int64{1, 3}) {
		t.Fatalf("want 1-3 matched, got %+v", matches)
	}
}
