package matching

import (
	"sort"
	"time"

	"reconpilot/internal/domain"
)

// MatchTolerant pairs rows that the exact pass could not tie by reference: an
// expected row with a bank line inside the amount and date tolerances, or an
// expected entry with the opposite entry that reverses it under the same
// reference. A pair is accepted only when neither row has another plausible
// partner of equal or stronger evidence. When the book offers a choice the
// pass does not make one; it returns the IDs of the rows left open while a
// partner still fits them as contested.
// Marketplace payouts are left to the group matcher.
//
// Evidence, strongest first: an agreeing reference, then the same amount. A
// same-amount pair without a reference must also have no rival: no other open
// bank line of that amount within AmountHorizonDays of the expected row, and
// no other open expected row within AmountHorizonDays of the bank line. Two
// kinds of unreferenced pair are never matched: amounts that agree only within
// the tolerance, and a credit and debit of one amount, which look like a
// reversal but could as well be two unrelated entries. Their rows are
// returned as contested when nothing stronger takes them.
//
// Candidate pairs are walked again for each step instead of being stored: a
// book with thousands of equal unreferenced rows has millions of them.
func MatchTolerant(p *Pool) (matches []Match, contested []int64) {
	type edge struct {
		other    int // index into p.Actual, or into p.Expected for a reversal
		reversal bool
		tier     int
		accept   bool
		amtDiff  int64
		days     int
	}
	const (
		tierRef = iota
		tierAmount
		tierTolerance // never accepted, so the loop below stops before it
	)

	type bankKey struct {
		cp  string
		dir domain.Direction
	}
	bank := map[bankKey][]int{} // indices into p.Actual, in date order
	for j, a := range p.Actual {
		if a.Source == domain.SourceBank {
			k := bankKey{a.CounterpartyRef, a.Direction}
			bank[k] = append(bank[k], j)
		}
	}
	type debitKey struct {
		cp     string
		amount int64
	}
	debits := map[debitKey][]int{} // expected debits, in date order
	for i, e := range p.Expected {
		if e.Direction == domain.DirDebit && e.AmountKurus > 0 {
			k := debitKey{e.CounterpartyRef, e.AmountKurus}
			debits[k] = append(debits[k], i)
		}
	}
	// Rows of one counterparty, direction and amount, in date order, for the
	// rivals an unreferenced pair must not have within AmountHorizonDays.
	type amountKey struct {
		cp     string
		dir    domain.Direction
		amount int64
	}
	bankByAmount, expByAmount := map[amountKey][]int{}, map[amountKey][]int{}
	for j, a := range p.Actual {
		if a.Source == domain.SourceBank {
			k := amountKey{a.CounterpartyRef, a.Direction, a.AmountKurus}
			bankByAmount[k] = append(bankByAmount[k], j)
		}
	}
	for i, e := range p.Expected {
		k := amountKey{e.CounterpartyRef, e.Direction, e.AmountKurus}
		expByAmount[k] = append(expByAmount[k], i)
	}
	tierOf := func(agree bool, amtDiff int64) int {
		switch {
		case agree:
			return tierRef
		case amtDiff == 0:
			return tierAmount
		}
		return tierTolerance
	}

	matchedExp := make([]bool, len(p.Expected))
	matchedAct := make([]bool, len(p.Actual))
	// edgesOf calls fn for each candidate pair of open expected row i whose
	// partner is open too: the bank lines in date order, then, for a credit,
	// the debits that may reverse it. It stops when fn returns false.
	edgesOf := func(i int, fn func(ed edge) bool) {
		if matchedExp[i] {
			return
		}
		e := p.Expected[i]
		maxAmtDiff := domain.TolerantLimit(e.AmountKurus)
		for _, j := range near(p.Actual, bank[bankKey{e.CounterpartyRef, e.Direction}], e.OccurredAt, domain.MatchWindowDays) {
			a := p.Actual[j]
			days := dayDiff(a.OccurredAt, e.OccurredAt)
			amtDiff := abs64(a.AmountKurus - e.AmountKurus)
			if matchedAct[j] || days > domain.MatchWindowDays || amtDiff > maxAmtDiff || p.RefConflict(e, a) {
				continue
			}
			agree := RefsAgree(e, a)
			if !fn(edge{other: j, tier: tierOf(agree, amtDiff), accept: agree || amtDiff == 0, amtDiff: amtDiff, days: days}) {
				return
			}
		}
		if e.Direction != domain.DirCredit || e.AmountKurus == 0 {
			return
		}
		for _, k := range near(p.Expected, debits[debitKey{e.CounterpartyRef, e.AmountKurus}], e.OccurredAt, domain.MatchWindowDays) {
			d := p.Expected[k]
			days := dayDiff(d.OccurredAt, e.OccurredAt)
			if matchedExp[k] || days > domain.MatchWindowDays {
				continue
			}
			agree := RefsAgree(e, d)
			if !agree && (e.ExternalRef != "" || d.ExternalRef != "") {
				continue // different references: two business records
			}
			if !fn(edge{other: k, reversal: true, tier: tierOf(agree, 0), accept: agree, days: days}) {
				return
			}
		}
	}
	// rivalFree reports whether an unreferenced same-amount pair is also the
	// only fit within the horizon: no other open bank line could be the
	// expected row's money and no other open expected row could be the bank
	// line's source.
	rivalFree := func(i int, ed edge) bool {
		e, a := p.Expected[i], p.Actual[ed.other]
		k := amountKey{e.CounterpartyRef, e.Direction, e.AmountKurus}
		for _, j := range near(p.Actual, bankByAmount[k], e.OccurredAt, domain.AmountHorizonDays) {
			if j != ed.other && !matchedAct[j] && dayDiff(p.Actual[j].OccurredAt, e.OccurredAt) <= domain.AmountHorizonDays &&
				!p.RefConflict(e, p.Actual[j]) {
				return false
			}
		}
		for _, x := range near(p.Expected, expByAmount[k], a.OccurredAt, domain.AmountHorizonDays) {
			if x != i && !matchedExp[x] && dayDiff(p.Expected[x].OccurredAt, a.OccurredAt) <= domain.AmountHorizonDays &&
				!p.RefConflict(p.Expected[x], a) {
				return false
			}
		}
		return true
	}

	var usedExp, usedAct []int
	for tier := tierRef; tier <= tierAmount; tier++ {
		degExp := make([]int, len(p.Expected))
		degAct := make([]int, len(p.Actual))
		for i := range p.Expected {
			edgesOf(i, func(ed edge) bool {
				if ed.tier <= tier {
					degExp[i]++
					if ed.reversal {
						degExp[ed.other]++
					} else {
						degAct[ed.other]++
					}
				}
				return true
			})
		}
		for i := range p.Expected {
			if degExp[i] != 1 {
				continue
			}
			e := p.Expected[i]
			edgesOf(i, func(ed edge) bool {
				if ed.tier > tier {
					return true
				}
				// The one pair of this row at this tier.
				if !ed.accept {
					return false
				}
				if ed.reversal {
					if degExp[ed.other] == 1 {
						matches = append(matches, Match{Kind: "reversal", ScoreBP: 10000 - int64(ed.days)*100,
							TxIDs: []int64{e.ID, p.Expected[ed.other].ID}})
						usedExp = append(usedExp, i, ed.other)
						matchedExp[i], matchedExp[ed.other] = true, true
					}
					return false
				}
				if degAct[ed.other] != 1 || (ed.tier != tierRef && !rivalFree(i, ed)) {
					return false
				}
				var amtBP int64
				if e.AmountKurus > 0 {
					amtBP = domain.BasisPoints(ed.amtDiff, e.AmountKurus)
				}
				matches = append(matches, Match{Kind: "tolerant",
					ScoreBP: 10000 - amtBP - int64(ed.days)*100,
					TxIDs:   []int64{e.ID, p.Actual[ed.other].ID}})
				usedExp = append(usedExp, i)
				usedAct = append(usedAct, ed.other)
				matchedExp[i], matchedAct[ed.other] = true, true
				return false
			})
		}
	}

	openExp := make([]bool, len(p.Expected))
	openAct := make([]bool, len(p.Actual))
	for i := range p.Expected {
		edgesOf(i, func(ed edge) bool {
			openExp[i] = true
			if ed.reversal {
				openExp[ed.other] = true
			} else {
				openAct[ed.other] = true
			}
			return true
		})
	}
	for i, e := range p.Expected {
		if openExp[i] {
			contested = append(contested, e.ID)
		}
	}
	for j, a := range p.Actual {
		if openAct[j] {
			contested = append(contested, a.ID)
		}
	}
	sort.Ints(usedExp)
	sort.Ints(usedAct)
	p.Expected = remove(p.Expected, usedExp)
	p.Actual = remove(p.Actual, usedAct)
	return matches, contested
}

// near returns the part of idx, which points into ts in date order, that
// holds every row at most days whole days from at, and a few just beyond;
// callers check the exact distance. The bound steps in fixed 24-hour days, as
// dayDiff counts them; a calendar step would move with a daylight-saving
// change in the rows' time zone. It does not allocate.
func near(ts []domain.Transaction, idx []int, at time.Time, days int) []int {
	lo := at.Add(-time.Duration(days+1) * 24 * time.Hour)
	start := sort.Search(len(idx), func(n int) bool { return !ts[idx[n]].OccurredAt.Before(lo) })
	end := start
	for end < len(idx) && dayDiff(ts[idx[end]].OccurredAt, at) <= days+1 {
		end++
	}
	return idx[start:end]
}

func abs64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}
