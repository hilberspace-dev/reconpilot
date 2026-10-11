package matching

import (
	"sort"

	"reconpilot/internal/domain"
)

// MatchTolerant considers bank transactions within the amount and date
// tolerances. Marketplace payouts are handled by the group matcher.
func MatchTolerant(p *Pool) []Match {
	var matches []Match
	var usedExp, usedAct []int
	claimed := map[int]bool{}
	for i, e := range p.Expected {
		best, bestAmtDiff, bestDayDiff := -1, int64(0), 0
		maxAmtDiff := domain.TolerantLimit(e.AmountKurus)
		for j, a := range p.Actual {
			if claimed[j] || a.Source != domain.SourceBank ||
				a.CounterpartyRef != e.CounterpartyRef || a.Direction != e.Direction {
				continue
			}
			amtDiff := abs64(a.AmountKurus - e.AmountKurus)
			days := dayDiff(a.OccurredAt, e.OccurredAt)
			if amtDiff > maxAmtDiff || days > domain.MatchWindowDays {
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
			amtBP = domain.BasisPoints(bestAmtDiff, e.AmountKurus)
		}
		matches = append(matches, Match{Kind: "tolerant",
			ScoreBP: 10000 - amtBP - int64(bestDayDiff)*100,
			TxIDs:   []int64{e.ID, p.Actual[best].ID}})
		usedExp = append(usedExp, i)
		usedAct = append(usedAct, best)
	}
	sort.Ints(usedAct)
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
