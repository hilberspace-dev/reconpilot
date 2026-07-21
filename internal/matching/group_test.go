package matching

import (
	"fmt"
	"testing"
	"time"

	"reconpilot/internal/domain"
)

func payout(id int64, counterparty string, gross, commission int64, day int) domain.Transaction {
	return domain.Transaction{
		ID: id, Source: domain.SourceMarketplace, ExternalRef: fmt.Sprintf("PAY-%d", id),
		CounterpartyRef: counterparty, AmountKurus: gross - commission,
		GrossKurus: gross, CommissionKurus: commission, Currency: "TRY",
		OccurredAt: time.Date(2026, 7, day, 0, 0, 0, 0, time.UTC),
		Direction:  domain.DirCredit, Status: domain.StatusUnmatched,
	}
}

func TestMatchGroupsFindsSubset(t *testing.T) {
	// Orders 1000+2000+3000; payout gross 3000 (orders 1 and 2), commission 450.
	p := SplitPool([]domain.Transaction{
		tx(1, domain.SourcePSP, "ORD-1", 1000, 1),
		tx(2, domain.SourcePSP, "ORD-2", 2000, 2),
		tx(3, domain.SourcePSP, "ORD-3", 3000, 2),
		payout(9, "seller-1", 3000, 450, 5),
	})
	matches, cands := MatchGroups(&p)
	if len(cands) != 0 {
		t.Fatalf("no candidate overflow expected: %v", cands)
	}
	if len(matches) != 1 || matches[0].Kind != "group" {
		t.Fatalf("want 1 group match, got %+v", matches)
	}
	if len(matches[0].TxIDs) != 3 { // ORD-1, ORD-2, PAY-9
		t.Fatalf("want members {1,2,9}, got %v", matches[0].TxIDs)
	}
	if len(p.Expected) != 1 { // ORD-3 remains
		t.Fatalf("ORD-3 must remain, pool=%d", len(p.Expected))
	}
}

func TestMatchGroupsOverflowFallsBack(t *testing.T) {
	// 21 candidate orders in window > 20 cap → payout becomes group_candidate.
	var txs []domain.Transaction
	for i := int64(1); i <= 21; i++ {
		txs = append(txs, tx(i, domain.SourcePSP, fmt.Sprintf("ORD-%d", i), 100, 1))
	}
	txs = append(txs, payout(99, "seller-1", 500, 50, 5))
	p := SplitPool(txs)
	matches, cands := MatchGroups(&p)
	if len(matches) != 0 || len(cands) != 1 || cands[0] != 99 {
		t.Fatalf("want overflow fallback: matches=%v cands=%v", matches, cands)
	}
}

// Regression: a PSP debit line (refund) must never be absorbed into a
// payout group as a POSITIVE addend — gross decomposes into credit order
// amounts only. Without the direction filter, payout gross 1500 falsely
// matches {credit 1000, debit 500}.
func TestMatchGroupsIgnoresDebitOrders(t *testing.T) {
	refund := tx(2, domain.SourcePSP, "REF-1", 500, 2)
	refund.Direction = domain.DirDebit
	p := SplitPool([]domain.Transaction{
		tx(1, domain.SourcePSP, "ORD-1", 1000, 1),
		refund,
		payout(9, "seller-1", 1500, 100, 5),
	})
	matches, cands := MatchGroups(&p)
	if len(matches) != 0 || len(cands) != 0 {
		t.Fatalf("debit refund absorbed as positive addend: matches=%+v cands=%v", matches, cands)
	}
	if len(p.Actual) != 1 || len(p.Expected) != 2 {
		t.Fatalf("nothing may leave the pool: expected=%d actual=%d", len(p.Expected), len(p.Actual))
	}
}

// Regression: a zero-gross payout must not trivially "match" the empty
// subset and produce a single-member group. It stays for classification.
func TestMatchGroupsZeroGrossPayoutNotMatched(t *testing.T) {
	p := SplitPool([]domain.Transaction{
		tx(1, domain.SourcePSP, "ORD-1", 1000, 1),
		payout(9, "seller-1", 0, 0, 5),
	})
	matches, cands := MatchGroups(&p)
	if len(matches) != 0 || len(cands) != 0 {
		t.Fatalf("zero-gross payout must not match: matches=%+v cands=%v", matches, cands)
	}
	if len(p.Actual) != 1 {
		t.Fatal("zero-gross payout must stay in pool for classification")
	}
}

// Two payouts of the same seller with disjoint intended subsets: both must
// match and never claim the same order twice (claimedExp bookkeeping).
// Power-of-two amounts make each target's subset unique.
func TestMatchGroupsTwoPayoutsDisjointSubsets(t *testing.T) {
	p := SplitPool([]domain.Transaction{
		tx(1, domain.SourcePSP, "ORD-1", 100, 1),
		tx(2, domain.SourcePSP, "ORD-2", 200, 2),
		tx(3, domain.SourcePSP, "ORD-3", 400, 3),
		tx(4, domain.SourcePSP, "ORD-4", 800, 4),
		payout(8, "seller-1", 300, 30, 5),   // ORD-1 + ORD-2
		payout(9, "seller-1", 1200, 120, 5), // ORD-3 + ORD-4
	})
	matches, cands := MatchGroups(&p)
	if len(cands) != 0 || len(matches) != 2 {
		t.Fatalf("want 2 group matches, got matches=%+v cands=%v", matches, cands)
	}
	seen := map[int64]bool{}
	for _, m := range matches {
		for _, id := range m.TxIDs {
			if seen[id] {
				t.Fatalf("transaction %d claimed by two groups: %+v", id, matches)
			}
			seen[id] = true
		}
	}
	for _, id := range []int64{1, 2, 3, 4, 8, 9} {
		if !seen[id] {
			t.Fatalf("transaction %d not claimed: %+v", id, matches)
		}
	}
	if len(p.Expected) != 0 || len(p.Actual) != 0 {
		t.Fatalf("pool must be empty: expected=%d actual=%d", len(p.Expected), len(p.Actual))
	}
}

func TestMatchGroupsNoSubsetLeavesPayout(t *testing.T) {
	p := SplitPool([]domain.Transaction{
		tx(1, domain.SourcePSP, "ORD-1", 1000, 1),
		payout(9, "seller-1", 999, 100, 5), // no subset sums to 999
	})
	matches, cands := MatchGroups(&p)
	if len(matches) != 0 || len(cands) != 0 {
		t.Fatalf("unexpected: %v %v", matches, cands)
	}
	if len(p.Actual) != 1 {
		t.Fatal("unmatched payout must stay in pool for classification")
	}
}
