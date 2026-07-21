package matching

// MatchExact pairs one expected with one actual transaction when
// ExternalRef, AmountKurus and Direction are equal and the value dates sit
// within matchWindowDays. Each actual is claimed at most once. The date
// bound keeps late same-reference payments visible as timing discrepancies
// instead of silently matching them.
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
				dayDiff(a.OccurredAt, e.OccurredAt) > matchWindowDays {
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
	sortInts(usedAct)
	p.Expected = remove(p.Expected, usedExp)
	p.Actual = remove(p.Actual, usedAct)
	return matches
}

func sortInts(a []int) {
	for i := 1; i < len(a); i++ {
		for j := i; j > 0 && a[j] < a[j-1]; j-- {
			a[j], a[j-1] = a[j-1], a[j]
		}
	}
}
