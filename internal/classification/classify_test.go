package classification

import (
	"testing"
	"time"

	"reconpilot/internal/domain"
	"reconpilot/internal/matching"
)

func tx(id int64, src domain.SourceType, ref string, amount int64, day int, dir domain.Direction) domain.Transaction {
	return domain.Transaction{
		ID: id, Source: src, ExternalRef: ref, CounterpartyRef: "seller-1",
		AmountKurus: amount, Currency: "TRY",
		OccurredAt: time.Date(2026, 7, day, 0, 0, 0, 0, time.UTC),
		Direction:  dir, Status: domain.StatusUnmatched,
	}
}

func typesOf(ds []Discrepancy) map[string]int {
	m := map[string]int{}
	for _, d := range ds {
		m[d.Type]++
	}
	return m
}

func TestClassifySevenTypes(t *testing.T) {
	all := []domain.Transaction{
		// duplicate pair on PSP side: 11 is original, 12 the duplicate
		tx(11, domain.SourcePSP, "ORD-D", 500, 1, domain.DirCredit),
		tx(12, domain.SourcePSP, "ORD-D", 500, 1, domain.DirCredit),
		// refund: bank debit referencing a known ref
		tx(21, domain.SourceBank, "ORD-D", 500, 3, domain.DirDebit),
		// partial: expected 1000, actual 400 (60% short → partial, not commission)
		tx(31, domain.SourcePSP, "ORD-P", 1000, 1, domain.DirCredit),
		tx(32, domain.SourceBank, "ORD-P", 400, 2, domain.DirCredit),
		// commission-like fee: expected 1000, actual 850 (15% → commission)
		tx(41, domain.SourcePSP, "ORD-C", 1000, 1, domain.DirCredit),
		tx(42, domain.SourceBank, "ORD-C", 850, 2, domain.DirCredit),
		// timing: same counterparty+amount, 6 days apart
		tx(51, domain.SourcePSP, "ORD-T", 777, 1, domain.DirCredit),
		tx(52, domain.SourceBank, "STMT-T", 777, 7, domain.DirCredit),
		// missing: expected with no counterpart anywhere
		tx(61, domain.SourcePSP, "ORD-M", 999, 1, domain.DirCredit),
	}
	// Leftover pool: everything above except tx 11 (pretend 11 matched exactly).
	var leftover []domain.Transaction
	for _, t2 := range all {
		if t2.ID != 11 {
			leftover = append(leftover, t2)
		}
	}
	pool := matching.SplitPool(leftover)
	ds := Classify(pool, nil, []int64{70}, all) // 70 = a group_candidate payout id
	got := typesOf(ds)
	// Counterpart mirrors (delta 0): tx 32 → partial, tx 42 → commission,
	// tx 52 → timing. unknown: ONLY the group_candidate payout 70.
	want := map[string]int{"duplicate": 1, "refund": 1, "partial": 2, "commission": 2, "timing": 2, "missing": 1, "unknown": 1}
	for typ, n := range want {
		if got[typ] < n {
			t.Fatalf("type %s: want >= %d, got %d (all=%v)", typ, n, got[typ], got)
		}
	}
	// group_candidate payout must be classified unknown:
	foundGC := false
	for _, d := range ds {
		if d.TxID != nil && *d.TxID == 70 && d.Type == "unknown" {
			foundGC = true
		}
	}
	if !foundGC {
		t.Fatal("group_candidate payout 70 must yield an unknown discrepancy")
	}
	// Mirror rows must carry delta 0 — the money is accounted on the expected side.
	for _, d := range ds {
		if d.TxID != nil && (*d.TxID == 32 || *d.TxID == 42 || *d.TxID == 52) && d.DeltaKurus != 0 {
			t.Fatalf("counterpart mirror for tx %d must have delta 0, got %d", *d.TxID, d.DeltaKurus)
		}
	}
}

// Regression (found by the benchmark): a refund debit whose credit leg sits
// on the SAME side with the same ref+amount must classify as refund, not as
// a duplicate of that credit leg — duplicates require equal direction.
func TestRefundNotMistakenForDuplicateOfItsCreditLeg(t *testing.T) {
	all := []domain.Transaction{
		tx(1, domain.SourcePSP, "ORD-R", 500, 1, domain.DirCredit),
		tx(2, domain.SourceBank, "ORD-R", 500, 2, domain.DirCredit),
		tx(3, domain.SourceBank, "ORD-R", 500, 4, domain.DirDebit), // refund leg
	}
	// Pretend 1 and 2 matched exactly; only the refund is leftover.
	pool := matching.SplitPool([]domain.Transaction{all[2]})
	ds := Classify(pool, nil, nil, all)
	if len(ds) != 1 || ds[0].Type != "refund" || ds[0].DeltaKurus != -500 {
		t.Fatalf("want refund(-500), got %+v", ds)
	}
}
