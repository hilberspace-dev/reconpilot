package matching

import (
	"math"
	"sort"

	"reconpilot/internal/domain"
)

// MatchSplits pairs an expected row with two or more bank lines that carry its
// reference and add up to its amount exactly, as when an order is paid partly
// by voucher and partly by card, or settled in parts. All open lines of that
// counterparty, reference and direction inside the date window must take part,
// and no other open expected row may carry the reference; otherwise the rows
// are left for classification.
func MatchSplits(p *Pool) []Match {
	openExp := map[refKey]int{}
	for _, e := range p.Expected {
		if e.ExternalRef != "" {
			openExp[keyOf(e)]++
		}
	}
	parts := map[refKey][]int{} // indices into p.Actual, in date order
	for j, a := range p.Actual {
		if a.Source == domain.SourceBank && a.ExternalRef != "" && openExp[keyOf(a)] == 1 {
			parts[keyOf(a)] = append(parts[keyOf(a)], j)
		}
	}
	var matches []Match
	var usedExp, usedAct []int
	for i, e := range p.Expected {
		if e.ExternalRef == "" || openExp[keyOf(e)] != 1 {
			continue
		}
		var pick []int
		var sum int64
		for _, j := range parts[keyOf(e)] {
			a := p.Actual[j]
			if a.Direction != e.Direction || dayDiff(a.OccurredAt, e.OccurredAt) > domain.MatchWindowDays {
				continue
			}
			if a.AmountKurus > math.MaxInt64-sum {
				sum = -1 // cannot equal a nonnegative amount
				break
			}
			sum += a.AmountKurus
			pick = append(pick, j)
		}
		if len(pick) < 2 || sum != e.AmountKurus {
			continue
		}
		ids := []int64{e.ID}
		for _, j := range pick {
			ids = append(ids, p.Actual[j].ID)
		}
		matches = append(matches, Match{Kind: "split", ScoreBP: 10000, TxIDs: ids})
		usedExp = append(usedExp, i)
		usedAct = append(usedAct, pick...)
	}
	sort.Ints(usedAct)
	p.Expected = remove(p.Expected, usedExp)
	p.Actual = remove(p.Actual, usedAct)
	return matches
}
