// Package generator produces realistic synthetic reconciliation books with
// deliberately injected discrepancies and a ground-truth record of every
// injection — the raw material of the benchmark's honesty rule (design §2).
package generator

import (
	"fmt"
	"math/rand/v2"
	"time"

	"reconpilot/internal/domain"
)

type GroundTruth struct {
	InjectedByType map[string]int
	CleanPairs     int
	CleanGroups    int
	// IntendedGroup maps every transaction that SHOULD end up inside a match
	// to its intended group label (pair ref or payout ref). Transactions
	// absent from the map must never be matched. The benchmark verifies the
	// engine's matches against this — "0 false matches" is measured.
	IntendedGroup map[int64]string
}

func Generate(seed int64, n int) ([]domain.Transaction, GroundTruth) {
	rng := rand.New(rand.NewPCG(uint64(seed), uint64(seed)>>1))
	gt := GroundTruth{InjectedByType: map[string]int{}, IntendedGroup: map[int64]string{}}
	var out []domain.Transaction
	id := int64(0)
	nextID := func() int64 { id++; return id }
	day0 := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)

	// Realistic counterparty cardinality is load-bearing: with only a handful
	// of sellers, unrelated same-seller rows collide into the tolerant window
	// by pure chance and manufacture false matches real books would not have.
	sellers := make([]string, 0, 300)
	for i := 0; i < 300; i++ {
		sellers = append(sellers, fmt.Sprintf("seller-%03d", i))
	}

	mkTx := func(src domain.SourceType, ref, cp string, amount int64, day int, dir domain.Direction) domain.Transaction {
		at := day0.AddDate(0, 0, day)
		return domain.Transaction{ID: nextID(), Source: src, ExternalRef: ref,
			CounterpartyRef: cp, AmountKurus: amount, Currency: "TRY", OccurredAt: at,
			Direction: dir, Status: domain.StatusUnmatched,
			DedupKey: domain.DedupKey(src, ref, amount, at, dir)}
	}
	amount := func() int64 { return 1000 + rng.Int64N(500_000) }
	seller := func() string { return sellers[rng.IntN(len(sellers))] }
	day := func() int { return rng.IntN(30) }

	pairBudget := n * 70 / 100 / 2 // each type injected at i%100 slots ⇒ ~1% per type
	for i := 0; i < pairBudget; i++ {
		ref, cp, amt, d := fmt.Sprintf("ORD-%06d", i), seller(), amount(), day()
		shift := rng.IntN(4) // 0–3 days: clean timing window
		switch {
		case i%100 < 1: // inject commission (fee-band shortfall 8–25%)
			cut := amt * (800 + rng.Int64N(1700)) / 10000
			out = append(out, mkTx(domain.SourcePSP, ref, cp, amt, d, domain.DirCredit))
			out = append(out, mkTx(domain.SourceBank, ref, cp, amt-cut, d+shift, domain.DirCredit))
			gt.InjectedByType["commission"]++
		case i%100 < 2: // partial (30–70% shortfall)
			cut := amt * (3000 + rng.Int64N(4000)) / 10000
			out = append(out, mkTx(domain.SourcePSP, ref, cp, amt, d, domain.DirCredit))
			out = append(out, mkTx(domain.SourceBank, ref, cp, amt-cut, d+shift, domain.DirCredit))
			gt.InjectedByType["partial"]++
		case i%100 < 3: // timing (4–10 days)
			out = append(out, mkTx(domain.SourcePSP, ref, cp, amt, d, domain.DirCredit))
			out = append(out, mkTx(domain.SourceBank, ref, cp, amt, d+4+rng.IntN(7), domain.DirCredit))
			gt.InjectedByType["timing"]++
		case i%100 < 4: // duplicate PSP row
			orig := mkTx(domain.SourcePSP, ref, cp, amt, d, domain.DirCredit)
			out = append(out, orig)
			dup := mkTx(domain.SourcePSP, ref, cp, amt, d, domain.DirCredit)
			dup.DedupKey = dup.DedupKey + "-dup" // survives ingest dedup: simulates source re-export
			out = append(out, dup)
			bank := mkTx(domain.SourceBank, ref, cp, amt, d+shift, domain.DirCredit)
			out = append(out, bank)
			gt.IntendedGroup[orig.ID] = ref // the ORIGINAL pairs with the bank row; the dup stays out
			gt.IntendedGroup[bank.ID] = ref
			gt.InjectedByType["duplicate"]++
		case i%100 < 5: // missing counterpart
			out = append(out, mkTx(domain.SourcePSP, ref, cp, amt, d, domain.DirCredit))
			gt.InjectedByType["missing"]++
		case i%100 < 7 && gt.InjectedByType["refund"] < pairBudget*5/100: // refund 2–5%
			psp := mkTx(domain.SourcePSP, ref, cp, amt, d, domain.DirCredit)
			bank := mkTx(domain.SourceBank, ref, cp, amt, d+shift, domain.DirCredit)
			out = append(out, psp, bank)
			refund := mkTx(domain.SourceBank, ref+"-R", cp, amt, d+shift+2, domain.DirDebit)
			// refund row reuses the original ref so the classifier can tie it back:
			refund.ExternalRef = ref
			refund.DedupKey = domain.DedupKey(domain.SourceBank, ref+"|refund", amt, refund.OccurredAt, domain.DirDebit)
			out = append(out, refund)
			gt.IntendedGroup[psp.ID] = ref // the clean pair matches; the refund row stays out
			gt.IntendedGroup[bank.ID] = ref
			gt.InjectedByType["refund"]++
		case i%100 < 8: // unknown orphan on the actual side
			psp := mkTx(domain.SourcePSP, ref, cp, amt, d, domain.DirCredit)
			bank := mkTx(domain.SourceBank, ref, cp, amt, d+shift, domain.DirCredit)
			out = append(out, psp, bank)
			out = append(out, mkTx(domain.SourceBank, fmt.Sprintf("ALIEN-%06d", i), "stranger-9", amount(), day(), domain.DirCredit))
			gt.IntendedGroup[psp.ID] = ref
			gt.IntendedGroup[bank.ID] = ref
			gt.InjectedByType["unknown"]++
		default: // clean pair
			psp := mkTx(domain.SourcePSP, ref, cp, amt, d, domain.DirCredit)
			bank := mkTx(domain.SourceBank, ref, cp, amt, d+shift, domain.DirCredit)
			out = append(out, psp, bank)
			gt.IntendedGroup[psp.ID] = ref
			gt.IntendedGroup[bank.ID] = ref
			gt.CleanPairs++
		}
	}
	// Clean marketplace groups: 2–8 orders per payout, commission 8–25%.
	// Group sellers form their own pool, at most TWO groups per seller: with
	// overlapping payout windows the candidate set stays ≤16 < the 20 cap, so
	// clean groups are always matchable and the ground-truth check is exact.
	groupBudget := n * 15 / 100 / 5
	for g := 0; g < groupBudget; g++ {
		cp := fmt.Sprintf("gseller-%03d", g%750)
		label := fmt.Sprintf("PAY-%04d", g)
		k := 2 + rng.IntN(7)
		var gross int64
		baseDay := rng.IntN(20)
		for j := 0; j < k; j++ {
			amt := amount()
			gross += amt
			ord := mkTx(domain.SourcePSP, fmt.Sprintf("GRP-%04d-%d", g, j), cp, amt, baseDay+rng.IntN(5), domain.DirCredit)
			out = append(out, ord)
			gt.IntendedGroup[ord.ID] = label
		}
		commission := gross * (800 + rng.Int64N(1700)) / 10000
		p := mkTx(domain.SourceMarketplace, label, cp, gross-commission, baseDay+7, domain.DirCredit)
		p.GrossKurus, p.CommissionKurus = gross, commission
		out = append(out, p)
		gt.IntendedGroup[p.ID] = label
		gt.CleanGroups++
	}
	return out, gt
}
