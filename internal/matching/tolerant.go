package matching

import "reconpilot/internal/domain"

const tolerantAmountBP = 50 // ±0.5% of the expected amount, in basis points

// MatchTolerant pairs remaining expected transactions with BANK-source
// actuals of the same counterparty whose amount and date fall within the
// tolerance windows. Marketplace payouts are excluded: they reconcile via
// the group matcher, and letting one tolerant-match a single order would
// be a false match. Greedy and deterministic: expected are visited in pool
// order; the best candidate (smallest |amount diff|, then |date diff|) wins.
func MatchTolerant(p *Pool) []Match {
	var matches []Match
	var usedExp, usedAct []int
	claimed := map[int]bool{}
	for i, e := range p.Expected {
		best, bestAmtDiff, bestDayDiff := -1, int64(0), 0
		maxAmtDiff := e.AmountKurus * tolerantAmountBP / 10000
		for j, a := range p.Actual {
			if claimed[j] || a.Source != domain.SourceBank ||
				a.CounterpartyRef != e.CounterpartyRef || a.Direction != e.Direction {
				continue
			}
			amtDiff := abs64(a.AmountKurus - e.AmountKurus)
			days := dayDiff(a.OccurredAt, e.OccurredAt)
			if amtDiff > maxAmtDiff || days > matchWindowDays {
				continue
			}
			if best < 0 || amtDiff < bestAmtDiff || (amtDiff == bestAmtDiff && days < bestDayDiff) {
				best, bestAmtDiff, bestDayDiff = j, amtDiff, days
			}
		}
		if best < 0 {
			continue
		}
		claimed[best] = true
		var amtBP int64
		if e.AmountKurus > 0 {
			amtBP = bestAmtDiff * 10000 / e.AmountKurus
		}
		matches = append(matches, Match{Kind: "tolerant",
			ScoreBP: 10000 - amtBP - int64(bestDayDiff)*100,
			TxIDs:   []int64{e.ID, p.Actual[best].ID}})
		usedExp = append(usedExp, i)
		usedAct = append(usedAct, best)
	}
	sortInts(usedAct)
	p.Expected = remove(p.Expected, usedExp)
	p.Actual = remove(p.Actual, usedAct)
	return matches
}

func abs64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}
