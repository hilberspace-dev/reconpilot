// Package classification assigns one of the 7 discrepancy types to every
// transaction the matching chain could not place (design §3). Rules fire in
// a fixed order; the first hit wins, so the outcome is deterministic.
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
	timingDays      = 3
)

func Classify(leftover matching.Pool, matches []matching.Match, groupCandidates []int64, all []domain.Transaction) []Discrepancy {
	var out []Discrepancy

	// Rule 1 — commission on every matched marketplace payout (group-level).
	byID := map[int64]domain.Transaction{}
	for _, t := range all {
		byID[t.ID] = t
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

	// group_candidate payouts → unknown.
	for _, id := range groupCandidates {
		id := id
		amount := byID[id].AmountKurus
		out = append(out, Discrepancy{TxID: &id, Type: "unknown", DeltaKurus: amount})
	}

	classifyOne := func(t domain.Transaction) Discrepancy {
		id := t.ID
		// duplicate: an earlier identical row on the same side exists. Same
		// direction is required — a bank debit (refund) is NOT a duplicate
		// of its own credit leg.
		for _, o := range all {
			if o.ID < t.ID && o.Source == t.Source && o.Direction == t.Direction &&
				o.ExternalRef == t.ExternalRef && o.AmountKurus == t.AmountKurus {
				return Discrepancy{TxID: &id, Type: "duplicate", DeltaKurus: t.AmountKurus}
			}
		}
		// refund: actual-side debit referencing a known ref.
		if !t.Source.IsExpected() && t.Direction == domain.DirDebit {
			for _, o := range all {
				if o.ID != t.ID && o.ExternalRef == t.ExternalRef {
					return Discrepancy{TxID: &id, Type: "refund", DeltaKurus: -t.AmountKurus}
				}
			}
		}
		// counterpart mirror: the actual side of a partial/fee or timing pair.
		// Delta 0 — the shortfall is accounted on the expected side; this row
		// only records WHY the actual line is open, keeping `unknown` honest.
		if !t.Source.IsExpected() && t.Direction == domain.DirCredit {
			for _, o := range all {
				if o.Source.IsExpected() && o.ExternalRef == t.ExternalRef &&
					t.AmountKurus > 0 && t.AmountKurus < o.AmountKurus {
					delta := o.AmountKurus - t.AmountKurus
					bp := delta * 10000 / o.AmountKurus
					typ := "partial"
					if bp >= commissionMinBP && bp <= commissionMaxBP {
						typ = "commission"
					}
					return Discrepancy{TxID: &id, Type: typ, DeltaKurus: 0}
				}
			}
			for _, o := range all {
				if o.Source.IsExpected() && o.CounterpartyRef == t.CounterpartyRef &&
					o.AmountKurus == t.AmountKurus {
					days := int(t.OccurredAt.Sub(o.OccurredAt).Hours() / 24)
					if days < 0 {
						days = -days
					}
					if days > timingDays {
						return Discrepancy{TxID: &id, Type: "timing", DeltaKurus: 0}
					}
				}
			}
		}
		if t.Source.IsExpected() {
			// partial / tx-level commission: smaller actual with same ref.
			for _, o := range all {
				if o.ID != t.ID && !o.Source.IsExpected() && o.ExternalRef == t.ExternalRef &&
					o.AmountKurus > 0 && o.AmountKurus < t.AmountKurus {
					delta := t.AmountKurus - o.AmountKurus
					bp := delta * 10000 / t.AmountKurus
					typ := "partial"
					if bp >= commissionMinBP && bp <= commissionMaxBP {
						typ = "commission"
					}
					return Discrepancy{TxID: &id, Type: typ, DeltaKurus: delta}
				}
			}
			// timing: same counterparty + exact amount, but outside the window.
			for _, o := range all {
				if o.ID != t.ID && !o.Source.IsExpected() && o.CounterpartyRef == t.CounterpartyRef &&
					o.AmountKurus == t.AmountKurus {
					days := int(o.OccurredAt.Sub(t.OccurredAt).Hours() / 24)
					if days < 0 {
						days = -days
					}
					if days > timingDays {
						return Discrepancy{TxID: &id, Type: "timing", DeltaKurus: 0}
					}
				}
			}
			// missing: nothing on the actual side resembles this expected tx.
			found := false
			for _, o := range all {
				if o.ID != t.ID && !o.Source.IsExpected() &&
					(o.ExternalRef == t.ExternalRef ||
						(o.CounterpartyRef == t.CounterpartyRef && o.AmountKurus == t.AmountKurus)) {
					found = true
					break
				}
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
