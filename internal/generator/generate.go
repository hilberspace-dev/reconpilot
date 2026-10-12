// Package generator builds seeded transaction books with known discrepancies
// for reconciliation testing and benchmarks.
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
	// Clean shapes that need more than the reference and amount: a bank line
	// with no reference, an order paid in several bank lines, and a sale
	// voided under its own reference.
	UnreferencedPairs int
	CleanSplits       int
	CleanReversals    int
	// IntendedGroup maps every transaction that should end up inside a match
	// to its intended group label (pair ref or payout ref). A transaction
	// missing from the map must stay unmatched; the benchmark counts a match
	// that includes one as a false match.
	IntendedGroup map[int64]string
	// InjectedTx maps the transaction that must carry each injected
	// discrepancy to its type: the PSP row of a commission, partial, timing
	// or missing order, the copied row of a duplicate, the debit row of a
	// refund, the stray bank row of an unknown, all four rows of an ambiguous
	// pair of pairs, the late-cleared check pair (one timing, two ambiguous)
	// and every row of an ambiguous payout group.
	InjectedTx map[int64]string
}

func Generate(seed int64, n int) ([]domain.Transaction, GroundTruth) {
	rng := rand.New(rand.NewPCG(uint64(seed), uint64(seed)>>1))
	gt := GroundTruth{InjectedByType: map[string]int{}, IntendedGroup: map[int64]string{}, InjectedTx: map[int64]string{}}
	var out []domain.Transaction
	id := int64(0)
	nextID := func() int64 { id++; return id }
	day0 := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)

	// With only a handful of sellers, unrelated rows of the same seller would
	// often land inside each other's tolerant window by chance.
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

	// An injected discrepancy leaves rows that exact matching cannot pair.
	// Two such rows of the same seller from different orders can still meet:
	// inside the tolerant window they are a candidate pair, and with the same
	// amount within AmountHorizonDays one is a rival or a late payment of the
	// other. Either way the ground truth would be wrong. reserve records an
	// injection's open rows per seller, or refuses them when one would meet an
	// earlier open row; the caller then draws a new amount.
	type openRow struct {
		amount int64
		day    int
	}
	openExpected, openActual := map[string][]openRow{}, map[string][]openRow{}
	inWindow := func(e, a openRow) bool {
		days, diff := e.day-a.day, e.amount-a.amount
		if days < 0 {
			days = -days
		}
		if diff < 0 {
			diff = -diff
		}
		if diff == 0 && days <= domain.AmountHorizonDays {
			return true
		}
		return days <= domain.MatchWindowDays && diff <= domain.TolerantLimit(e.amount)
	}
	reserve := func(cp string, expected, actual []openRow) bool {
		for _, e := range expected {
			for _, a := range openActual[cp] {
				if inWindow(e, a) {
					return false
				}
			}
		}
		for _, a := range actual {
			for _, e := range openExpected[cp] {
				if inWindow(e, a) {
					return false
				}
			}
		}
		openExpected[cp] = append(openExpected[cp], expected...)
		openActual[cp] = append(openActual[cp], actual...)
		return true
	}

	// Budgets are scaled so the output row count lands near n (injection
	// shapes add/remove rows; unscaled, output would be ~90% of n).
	n = n * 110 / 100
	pairBudget := n * 70 / 100 / 2 // each type takes fixed i%100 slots: about 1 % each, refunds 2 %
	for i := 0; i < pairBudget; i++ {
		ref, cp, amt, d := fmt.Sprintf("ORD-%06d", i), seller(), amount(), day()
		shift := rng.IntN(4) // 0-3 days, inside the match window
		switch {
		case i%100 < 1: // commission: a fee-band shortfall of 8-25 %
			// Integer division can turn an 8.00 % draw into 7.99 %, which the
			// classifier correctly calls partial; one more kurus keeps the cut
			// inside the 8-25 % fee band.
			feeCut := func() int64 {
				cut := amt * (800 + rng.Int64N(1700)) / 10000
				if cut*10000/amt < 800 {
					cut++
				}
				return cut
			}
			cut := feeCut()
			for !reserve(cp, []openRow{{amt, d}}, []openRow{{amt - cut, d + shift}}) {
				amt = amount()
				cut = feeCut()
			}
			psp := mkTx(domain.SourcePSP, ref, cp, amt, d, domain.DirCredit)
			out = append(out, psp, mkTx(domain.SourceBank, ref, cp, amt-cut, d+shift, domain.DirCredit))
			gt.InjectedTx[psp.ID] = "commission"
			gt.InjectedByType["commission"]++
		case i%100 < 2: // partial: a 30-70 % shortfall
			cut := amt * (3000 + rng.Int64N(4000)) / 10000
			for !reserve(cp, []openRow{{amt, d}}, []openRow{{amt - cut, d + shift}}) {
				amt = amount()
				cut = amt * (3000 + rng.Int64N(4000)) / 10000
			}
			psp := mkTx(domain.SourcePSP, ref, cp, amt, d, domain.DirCredit)
			out = append(out, psp, mkTx(domain.SourceBank, ref, cp, amt-cut, d+shift, domain.DirCredit))
			gt.InjectedTx[psp.ID] = "partial"
			gt.InjectedByType["partial"]++
		case i%100 < 3: // timing: the bank row arrives 4-10 days later
			late := d + 4 + rng.IntN(7)
			for !reserve(cp, []openRow{{amt, d}}, []openRow{{amt, late}}) {
				amt = amount()
			}
			psp := mkTx(domain.SourcePSP, ref, cp, amt, d, domain.DirCredit)
			out = append(out, psp, mkTx(domain.SourceBank, ref, cp, amt, late, domain.DirCredit))
			gt.InjectedTx[psp.ID] = "timing"
			gt.InjectedByType["timing"]++
		case i%100 < 4: // duplicate PSP row; the copy stays open
			for !reserve(cp, []openRow{{amt, d}}, nil) {
				amt = amount()
			}
			orig := mkTx(domain.SourcePSP, ref, cp, amt, d, domain.DirCredit)
			out = append(out, orig)
			dup := mkTx(domain.SourcePSP, ref, cp, amt, d, domain.DirCredit)
			dup.DedupKey = dup.DedupKey + "-dup" // survives ingest dedup: simulates source re-export
			out = append(out, dup)
			bank := mkTx(domain.SourceBank, ref, cp, amt, d+shift, domain.DirCredit)
			out = append(out, bank)
			gt.IntendedGroup[orig.ID] = ref // the original pairs with the bank row; the dup stays out
			gt.IntendedGroup[bank.ID] = ref
			gt.InjectedTx[dup.ID] = "duplicate"
			gt.InjectedByType["duplicate"]++
		case i%100 < 5: // missing counterpart
			for !reserve(cp, []openRow{{amt, d}}, nil) {
				amt = amount()
			}
			psp := mkTx(domain.SourcePSP, ref, cp, amt, d, domain.DirCredit)
			out = append(out, psp)
			gt.InjectedTx[psp.ID] = "missing"
			gt.InjectedByType["missing"]++
		case i%100 < 7 && gt.InjectedByType["refund"] < pairBudget*5/100: // refund, capped at 5 %
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
			gt.InjectedTx[refund.ID] = "refund"
			gt.InjectedByType["refund"]++
		case i%100 < 8: // unknown orphan on the actual side
			psp := mkTx(domain.SourcePSP, ref, cp, amt, d, domain.DirCredit)
			bank := mkTx(domain.SourceBank, ref, cp, amt, d+shift, domain.DirCredit)
			out = append(out, psp, bank)
			alien := mkTx(domain.SourceBank, fmt.Sprintf("ALIEN-%06d", i), "stranger-9", amount(), day(), domain.DirCredit)
			out = append(out, alien)
			gt.IntendedGroup[psp.ID] = ref
			gt.IntendedGroup[bank.ID] = ref
			gt.InjectedTx[alien.ID] = "unknown"
			gt.InjectedByType["unknown"]++
		case i%100 < 9: // clean pair whose bank line carries no reference
			for !reserve(cp, []openRow{{amt, d}}, []openRow{{amt, d + shift}}) {
				amt = amount()
			}
			psp := mkTx(domain.SourcePSP, ref, cp, amt, d, domain.DirCredit)
			bank := mkTx(domain.SourceBank, "", cp, amt, d+shift, domain.DirCredit)
			bank.DedupKey = domain.DedupKey(domain.SourceBank, ref+"|noref", amt, bank.OccurredAt, domain.DirCredit)
			out = append(out, psp, bank)
			gt.IntendedGroup[psp.ID] = ref
			gt.IntendedGroup[bank.ID] = ref
			gt.UnreferencedPairs++
		case i%100 < 10: // two equal orders paid by two equal unreferenced lines
			// Either pairing fits, so the engine must leave all four rows open
			// as ambiguous instead of picking one.
			for !reserve(cp, []openRow{{amt, d}, {amt, d}}, []openRow{{amt, d + shift}, {amt, d + shift}}) {
				amt = amount()
			}
			for _, suffix := range []string{"-A", "-B"} {
				psp := mkTx(domain.SourcePSP, ref+suffix, cp, amt, d, domain.DirCredit)
				bank := mkTx(domain.SourceBank, "", cp, amt, d+shift, domain.DirCredit)
				bank.DedupKey = domain.DedupKey(domain.SourceBank, ref+suffix+"|noref", amt, bank.OccurredAt, domain.DirCredit)
				out = append(out, psp, bank)
				gt.InjectedTx[psp.ID] = "ambiguous"
				gt.InjectedTx[bank.ID] = "ambiguous"
			}
			gt.InjectedByType["ambiguous"] += 4
		case i%100 < 11: // an order paid in two or three bank lines
			psp := mkTx(domain.SourcePSP, ref, cp, amt, d, domain.DirCredit)
			out = append(out, psp)
			gt.IntendedGroup[psp.ID] = ref
			k := 2 + rng.IntN(2)
			left := amt
			for j := 0; j < k; j++ {
				part := left
				if j < k-1 {
					part = 1 + rng.Int64N(left-int64(k-1-j))
				}
				left -= part
				bank := mkTx(domain.SourceBank, ref, cp, part, d+rng.IntN(4), domain.DirCredit)
				bank.DedupKey = domain.DedupKey(domain.SourceBank, fmt.Sprintf("%s|part%d", ref, j), part, bank.OccurredAt, domain.DirCredit)
				out = append(out, bank)
				gt.IntendedGroup[bank.ID] = ref
			}
			gt.CleanSplits++
		case i%100 < 12: // a sale and its void under the same reference
			// Other open rows of the seller carry other references, so only
			// unreferenced bank lines could compete; reserve keeps them away.
			for !reserve(cp, []openRow{{amt, d}, {amt, d}}, nil) {
				amt = amount()
			}
			sale := mkTx(domain.SourcePSP, ref, cp, amt, d, domain.DirCredit)
			void := mkTx(domain.SourcePSP, ref, cp, amt, d+rng.IntN(2), domain.DirDebit)
			out = append(out, sale, void)
			gt.IntendedGroup[sale.ID] = ref
			gt.IntendedGroup[void.ID] = ref
			gt.CleanReversals++
		case i%100 < 13: // a partial payment whose bank line fits another order
			// Order ref is missing; order ref-Y was paid 30-70 % short, and its
			// bank line carries ref-Y and exactly the amount of the missing
			// order. The line names ref-Y, so it is not the missing order's
			// money.
			full := amount()
			cut := full * (3000 + rng.Int64N(4000)) / 10000
			for !reserve(cp, []openRow{{full - cut, d}, {full, d}}, []openRow{{full - cut, d + shift}}) {
				full = amount()
				cut = full * (3000 + rng.Int64N(4000)) / 10000
			}
			missing := mkTx(domain.SourcePSP, ref, cp, full-cut, d, domain.DirCredit)
			short := mkTx(domain.SourcePSP, ref+"-Y", cp, full, d, domain.DirCredit)
			out = append(out, missing, short, mkTx(domain.SourceBank, ref+"-Y", cp, full-cut, d+shift, domain.DirCredit))
			gt.InjectedTx[missing.ID] = "missing"
			gt.InjectedTx[short.ID] = "partial"
			gt.InjectedByType["missing"]++
			gt.InjectedByType["partial"]++
		case i%100 < 14: // two checks of one amount, both cleared late
			// Check A clears on day d+8, the day after check B was issued; B
			// clears on day d+16. Inside B's window A's clearance is the only
			// fit, but B's own clearance is within the horizon, so B and A's
			// clearance stay ambiguous; A and B's clearance are timing.
			for !reserve(cp, []openRow{{amt, d}, {amt, d + 7}}, []openRow{{amt, d + 8}, {amt, d + 16}}) {
				amt = amount()
			}
			checkA := mkTx(domain.SourcePSP, ref+"-A", cp, amt, d, domain.DirCredit)
			checkB := mkTx(domain.SourcePSP, ref+"-B", cp, amt, d+7, domain.DirCredit)
			clearA := mkTx(domain.SourceBank, "", cp, amt, d+8, domain.DirCredit)
			clearA.DedupKey = domain.DedupKey(domain.SourceBank, ref+"-A|noref", amt, clearA.OccurredAt, domain.DirCredit)
			clearB := mkTx(domain.SourceBank, "", cp, amt, d+16, domain.DirCredit)
			clearB.DedupKey = domain.DedupKey(domain.SourceBank, ref+"-B|noref", amt, clearB.OccurredAt, domain.DirCredit)
			out = append(out, checkA, checkB, clearA, clearB)
			gt.InjectedTx[checkA.ID] = "timing" // clearB is the delta-0 timing record
			gt.InjectedTx[checkB.ID] = "ambiguous"
			gt.InjectedTx[clearA.ID] = "ambiguous"
			gt.InjectedByType["timing"]++
			gt.InjectedByType["ambiguous"] += 2
		default: // clean pair
			psp := mkTx(domain.SourcePSP, ref, cp, amt, d, domain.DirCredit)
			bank := mkTx(domain.SourceBank, ref, cp, amt, d+shift, domain.DirCredit)
			out = append(out, psp, bank)
			gt.IntendedGroup[psp.ID] = ref
			gt.IntendedGroup[bank.ID] = ref
			gt.CleanPairs++
		}
	}
	// Clean marketplace groups: 2-8 orders per payout, commission 8-25 %.
	// Group sellers have their own pool and at most two payouts each. A
	// seller's second group is shifted 35 days later, so the two 14-day
	// payout windows never overlap; the payout dates end up 16 to 54 days
	// apart. If they did, another subset of the
	// seller's orders could add up to the same gross by chance and the group
	// matcher would have no way to tell which subset was meant.
	groupBudget := n * 15 / 100 / 5
	groupSellers := max(750, (groupBudget+1)/2)
	for g := 0; g < groupBudget; g++ {
		cp := fmt.Sprintf("gseller-%03d", g%groupSellers)
		label := fmt.Sprintf("PAY-%04d", g)
		k := 2 + rng.IntN(7)
		var gross int64
		baseDay := rng.IntN(20)
		if g >= groupSellers {
			baseDay += 35
		}
		var members []domain.Transaction
		for j := 0; j < k; j++ {
			amt := amount()
			gross += amt
			members = append(members, mkTx(domain.SourcePSP, fmt.Sprintf("GRP-%04d-%d", g, j), cp, amt, baseDay+rng.IntN(5), domain.DirCredit))
		}
		// One group in twenty gets a stray order of the seller with the same
		// amount as one of its orders. Two sets of orders then add up to the
		// gross, and the payout, its orders and the stray must stay open as
		// ambiguous.
		ambiguous := g%20 == 19
		if ambiguous {
			members = append(members, mkTx(domain.SourcePSP, fmt.Sprintf("GRP-%04d-x", g), cp, members[0].AmountKurus, baseDay+rng.IntN(5), domain.DirCredit))
		}
		commission := gross * (800 + rng.Int64N(1700)) / 10000
		p := mkTx(domain.SourceMarketplace, label, cp, gross-commission, baseDay+7, domain.DirCredit)
		p.GrossKurus, p.CommissionKurus = gross, commission
		members = append(members, p)
		out = append(out, members...)
		for _, m := range members {
			if ambiguous {
				gt.InjectedTx[m.ID] = "ambiguous"
				gt.InjectedByType["ambiguous"]++
			} else {
				gt.IntendedGroup[m.ID] = label
			}
		}
		if !ambiguous {
			gt.CleanGroups++
		}
	}
	return out, gt
}
