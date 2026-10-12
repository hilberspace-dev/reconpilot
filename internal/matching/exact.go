package matching

import (
	"sort"

	"reconpilot/internal/domain"
)

// MatchExact pairs transactions by reference, amount and direction within
// the allowed date window. Late payments remain for discrepancy classification.
// Rows without a reference are left to the tolerant pass: an empty reference
// is the absence of evidence, and two of them must not count as a match.
func MatchExact(p *Pool) []Match {
	type key struct {
		ref    refKey
		amount int64
	}
	actualByKey := map[key][]int{}
	for i, a := range p.Actual {
		if a.ExternalRef == "" {
			continue
		}
		k := key{keyOf(a), a.AmountKurus}
		actualByKey[k] = append(actualByKey[k], i)
	}
	var matches []Match
	var usedExp, usedAct []int
	claimed := map[int]bool{}
	for i, e := range p.Expected {
		if e.ExternalRef == "" {
			continue
		}
		k := key{keyOf(e), e.AmountKurus}
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
