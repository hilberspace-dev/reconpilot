// Package classification assigns one of the eight discrepancy types to every
// transaction the matching passes could not place. Rules run in a fixed order
// and the first rule that applies wins.
package classification

import (
	"reconpilot/internal/domain"
	"reconpilot/internal/matching"
)

type Discrepancy struct {
	TxID       *int64
	MatchIdx   *int // index into the matches slice; store maps to match_group_id
	Type       string
	DeltaKurus int64
}

const (
	commissionMinBP = 800  // 8%
	commissionMaxBP = 2500 // 25%
)

// Classify types the leftover rows. ambiguous lists the rows the matching
// passes left open on purpose: more than one partner fitted, or the only
// partner did not carry enough evidence. They are typed ambiguous unless a
// late split or a duplicate explains them first.
func Classify(leftover matching.Pool, matches []matching.Match, groupCandidates, ambiguous []int64, all []domain.Transaction) []Discrepancy {
	var out []Discrepancy
	ordered := matching.SplitPool(all)
	all = append(ordered.Expected, ordered.Actual...)

	// Every matched marketplace payout gets a group-level commission record.
	byID := map[int64]domain.Transaction{}
	rank := map[int64]int{} // position in the sorted book
	for i, t := range all {
		byID[t.ID] = t
		rank[t.ID] = i
	}
	for mi, m := range matches {
		if m.Kind != "group" {
			continue
		}
		for _, id := range m.TxIDs {
			if t, ok := byID[id]; ok && t.Source == domain.SourceMarketplace && t.CommissionKurus > 0 {
				idx := mi
				out = append(out, Discrepancy{MatchIdx: &idx, Type: "commission", DeltaKurus: t.CommissionKurus})
			}
		}
	}

	// Payouts with too many candidate orders are classified as unknown.
	for _, id := range groupCandidates {
		amount := byID[id].AmountKurus
		out = append(out, Discrepancy{TxID: &id, Type: "unknown", DeltaKurus: amount})
	}

	unsure := map[int64]bool{}
	for _, id := range ambiguous {
		unsure[id] = true
	}
	// The rules look rows up by reference or by counterparty and amount. Each
	// list keeps the order of all (expected rows first, each side sorted), so
	// "the first row that fits" is the same row a scan of the book would find.
	type cpAmount struct {
		cp     string
		amount int64
		dir    domain.Direction
	}
	// A reference names a record within one counterparty only.
	type cpRef struct{ cp, ref string }
	refOf := func(t domain.Transaction) cpRef { return cpRef{t.CounterpartyRef, t.ExternalRef} }
	byRef := map[cpRef][]domain.Transaction{}
	byCpAmount := map[cpAmount][]domain.Transaction{}
	for _, t := range all {
		if t.ExternalRef != "" {
			byRef[refOf(t)] = append(byRef[refOf(t)], t)
		}
		k := cpAmount{t.CounterpartyRef, t.AmountKurus, t.Direction}
		byCpAmount[k] = append(byCpAmount[k], t)
	}
	days := func(a, b domain.Transaction) int {
		d := int(a.OccurredAt.Sub(b.OccurredAt).Hours() / 24)
		if d < 0 {
			return -d
		}
		return d
	}
	// late reports whether actual is the same money as expected arriving
	// outside the window. Without an agreeing reference the amount is the only
	// evidence, and it reaches AmountHorizonDays.
	late := func(expected, actual domain.Transaction) bool {
		if ordered.RefConflict(expected, actual) {
			return false
		}
		d := days(actual, expected)
		return d > domain.MatchWindowDays && (matching.RefsAgree(expected, actual) || d <= domain.AmountHorizonDays)
	}
	// lateSplit reports whether t belongs to a split settlement whose bank
	// parts add up to the expected amount but arrived outside the window, so
	// the split pass left it open. Like a single late payment, that is timing.
	lateSplit := func(t domain.Transaction) bool {
		var exp []domain.Transaction
		for _, o := range byRef[refOf(t)] {
			if o.Source.IsExpected() {
				exp = append(exp, o)
			}
		}
		if len(exp) != 1 {
			return false
		}
		isPart := func(o domain.Transaction) bool {
			return o.Source == domain.SourceBank && o.Direction == exp[0].Direction && o.CounterpartyRef == exp[0].CounterpartyRef
		}
		if t.ID != exp[0].ID && !isPart(t) {
			return false
		}
		var sum int64
		parts, outside := 0, false
		for _, o := range byRef[refOf(t)] {
			if isPart(o) {
				sum += o.AmountKurus
				parts++
				outside = outside || days(o, exp[0]) > domain.MatchWindowDays
			}
		}
		return parts >= 2 && outside && sum == exp[0].AmountKurus
	}
	shortfall := func(id int64, expected, actual int64, delta int64) Discrepancy {
		typ := "partial"
		if bp := domain.BasisPoints(expected-actual, expected); bp >= commissionMinBP && bp <= commissionMaxBP {
			typ = "commission"
		}
		return Discrepancy{TxID: &id, Type: typ, DeltaKurus: delta}
	}

	classifyOne := func(t domain.Transaction) Discrepancy {
		id := t.ID
		// A late split comes first: two equal parts of one settlement are
		// both needed for the sum, so neither is a duplicate of the other.
		if lateSplit(t) {
			return Discrepancy{TxID: &id, Type: "timing", DeltaKurus: 0}
		}
		// duplicate: an earlier row on the same side (earlier in the sorted
		// book, not by ID) carries the same reference and amount. The
		// direction must match too, so a bank debit (a refund) is not taken
		// for a duplicate of its own credit leg.
		for _, o := range byRef[refOf(t)] {
			if rank[o.ID] < rank[t.ID] && o.Source == t.Source && o.Direction == t.Direction && o.AmountKurus == t.AmountKurus {
				return Discrepancy{TxID: &id, Type: "duplicate", DeltaKurus: t.AmountKurus}
			}
		}
		if unsure[t.ID] {
			return Discrepancy{TxID: &id, Type: "ambiguous", DeltaKurus: t.AmountKurus}
		}
		// refund: actual-side debit referencing a known ref.
		if !t.Source.IsExpected() && t.Direction == domain.DirDebit && len(byRef[refOf(t)]) > 1 {
			return Discrepancy{TxID: &id, Type: "refund", DeltaKurus: -t.AmountKurus}
		}

		// counterpart mirror: the actual side of a partial, fee or timing pair.
		// The shortfall is booked on the expected side, so this row has delta
		// 0. It records why the actual line is open instead of calling it
		// unknown.
		if !t.Source.IsExpected() && t.Direction == domain.DirCredit {
			for _, o := range byRef[refOf(t)] {
				if o.Source.IsExpected() && o.Direction == t.Direction &&
					t.AmountKurus > 0 && t.AmountKurus < o.AmountKurus {
					return shortfall(id, o.AmountKurus, t.AmountKurus, 0)
				}
			}
			for _, o := range byCpAmount[cpAmount{t.CounterpartyRef, t.AmountKurus, t.Direction}] {
				if o.Source.IsExpected() && late(o, t) {
					return Discrepancy{TxID: &id, Type: "timing", DeltaKurus: 0}
				}
			}
		}
		if t.Source.IsExpected() {
			// partial / tx-level commission: smaller actual with same ref.
			for _, o := range byRef[refOf(t)] {
				if !o.Source.IsExpected() && o.Direction == t.Direction &&
					o.AmountKurus > 0 && o.AmountKurus < t.AmountKurus {
					return shortfall(id, t.AmountKurus, o.AmountKurus, t.AmountKurus-o.AmountKurus)
				}
			}
			// timing: same counterparty + exact amount, but outside the window.
			sameAmount := byCpAmount[cpAmount{t.CounterpartyRef, t.AmountKurus, t.Direction}]
			for _, o := range sameAmount {
				if !o.Source.IsExpected() && late(t, o) {
					return Discrepancy{TxID: &id, Type: "timing", DeltaKurus: 0}
				}
			}
			// missing: nothing on the actual side resembles this expected tx.
			found := false
			for _, o := range byRef[refOf(t)] {
				if !o.Source.IsExpected() && o.Direction == t.Direction {
					found = true
					break
				}
			}
			for _, o := range sameAmount {
				if found {
					break
				}
				found = !o.Source.IsExpected() && !ordered.RefConflict(t, o) &&
					(matching.RefsAgree(t, o) || days(o, t) <= domain.AmountHorizonDays)
			}
			if !found {
				return Discrepancy{TxID: &id, Type: "missing", DeltaKurus: t.AmountKurus}
			}
		}
		return Discrepancy{TxID: &id, Type: "unknown", DeltaKurus: t.AmountKurus}
	}

	for _, t := range leftover.Expected {
		out = append(out, classifyOne(t))
	}
	for _, t := range leftover.Actual {
		out = append(out, classifyOne(t))
	}
	return out
}
