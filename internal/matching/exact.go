package matching

import (
	"sort"

	"reconpilot/internal/domain"
)

// MatchExact pairs transactions by reference, amount and direction within
// the allowed date window. Late payments remain for discrepancy classification.
func MatchExact(p *Pool) []Match {
	type key struct {
		ref    string
		amount int64
	}
	actualByKey := map[key][]int{}
	for i, a := range p.Actual {
		k := key{a.ExternalRef, a.AmountKurus}
		actualByKey[k] = append(actualByKey[k], i)
	}
	var matches []Match
	var usedExp, usedAct []int
	claimed := map[int]bool{}
	for i, e := range p.Expected {
		k := key{e.ExternalRef, e.AmountKurus}
		found := -1
		for _, ai := range actualByKey[k] {
			a := p.Actual[ai]
			if claimed[ai] || a.Direction != e.Direction ||
				dayDiff(a.OccurredAt, e.OccurredAt) > domain.MatchWindowDays {
				continue
			}
			found = ai
			break
		}
		if found < 0 {
			continue
		}
		claimed[found] = true
		matches = append(matches, Match{Kind: "exact", ScoreBP: 10000,
			TxIDs: []int64{e.ID, p.Actual[found].ID}})
		usedExp = append(usedExp, i)
		usedAct = append(usedAct, found)
	}
	sort.Ints(usedAct)
	p.Expected = remove(p.Expected, usedExp)
	p.Actual = remove(p.Actual, usedAct)
	return matches
}
