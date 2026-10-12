package matching

import (
	"math"
	"sort"
	"time"

	"reconpilot/internal/domain"
)

const (
	groupWindowDays = 14 // payout period: orders up to 14 days before payout
	groupMemberCap  = 20 // larger candidate sets are classified as unknown (ADR 0003)
)

// MatchGroups reconciles marketplace payouts against groups of orders.
// Searches above groupMemberCap return an explicit unknown classification.
// When two different sets of orders add up to the gross, the payout and the
// orders of the first two sets found are returned as ambiguous and nothing is
// matched. Those orders stay contested: a later payout whose fitting set uses
// one of them is refused the same way, since taking it would settle the first
// payout by elimination, and the orders of its own sets become contested in
// turn. Leaving contested orders out of the later search instead would hide
// that payout's own set when it contains one. Payouts are searched in the
// pool's order (date, then reference), so an earlier payout whose only
// fitting set takes an order wins it before a later payout is searched.
func MatchGroups(p *Pool) (matches []Match, groupCandidates, ambiguous []int64) {
	var usedAct []int
	claimedExp := map[int]bool{}
	contestedExp := map[int]bool{}
	// Credit orders by counterparty, in pool order, so a payout looks only
	// at its own seller's orders. The gross is the sum of credit order
	// amounts: a debit (a refund) added as a positive amount would make a
	// false match, and subsetSum's pruning assumes positive amounts anyway.
	orders := map[string][]int{}
	for ei, e := range p.Expected {
		if e.Direction == domain.DirCredit && e.AmountKurus > 0 {
			orders[e.CounterpartyRef] = append(orders[e.CounterpartyRef], ei)
		}
	}
	for ai, payout := range p.Actual {
		if payout.Source != domain.SourceMarketplace {
			continue
		}
		// Only credit payouts with a positive gross decompose into order
		// sums. A zero gross would trivially "match" the empty subset and
		// produce a single-member group; anything else stays in the pool
		// for classification.
		if payout.Direction != domain.DirCredit || payout.GrossKurus <= 0 {
			continue
		}
		lo := payout.OccurredAt.Add(-groupWindowDays * 24 * time.Hour)
		var cand []int // indices into p.Expected
		for _, ei := range orders[payout.CounterpartyRef] {
			e := p.Expected[ei]
			if claimedExp[ei] || e.OccurredAt.Before(lo) || e.OccurredAt.After(payout.OccurredAt) {
				continue
			}
			cand = append(cand, ei)
		}
		if len(cand) > groupMemberCap {
			groupCandidates = append(groupCandidates, payout.ID)
			usedAct = append(usedAct, ai)
			continue
		}
		subset, other, found := subsetSum(p.Expected, cand, payout.GrossKurus)
		if found == 0 {
			continue
		}
		touchesContested := false
		for _, ei := range subset {
			touchesContested = touchesContested || contestedExp[ei]
		}
		if found > 1 || touchesContested {
			ambiguous = append(ambiguous, payout.ID)
			for _, ei := range append(subset, other...) {
				if !contestedExp[ei] {
					contestedExp[ei] = true
					ambiguous = append(ambiguous, p.Expected[ei].ID)
				}
			}
			continue
		}
		ids := make([]int64, 0, len(subset)+1)
		for _, ei := range subset {
			claimedExp[ei] = true
			ids = append(ids, p.Expected[ei].ID)
		}
		ids = append(ids, payout.ID)
		matches = append(matches, Match{Kind: "group", ScoreBP: 10000, TxIDs: ids})
		usedAct = append(usedAct, ai)
	}
	var usedExp []int
	for ei := range claimedExp {
		usedExp = append(usedExp, ei)
	}
	sort.Ints(usedExp)
	sort.Ints(usedAct)
	p.Expected = remove(p.Expected, usedExp)
	p.Actual = remove(p.Actual, usedAct)
	return matches, groupCandidates, ambiguous
}

// subsetSum finds the first subset equal to target, preferring older
// transactions, and keeps searching for a second one. It returns both and how
// many it found (0, 1 or 2). The caller filters out non-positive amounts.
// Search is bounded by groupMemberCap (worst case 2^20 combinations).
func subsetSum(txs []domain.Transaction, cand []int, target int64) (first, second []int, found int) {
	suffix := make([]int64, len(cand)+1)
	for i := len(cand) - 1; i >= 0; i-- {
		amount := txs[cand[i]].AmountKurus
		if amount > math.MaxInt64-suffix[i+1] {
			suffix[i] = math.MaxInt64
		} else {
			suffix[i] = suffix[i+1] + amount
		}
	}
	var pick []int
	var walk func(i int, remaining int64) bool // true stops the search
	walk = func(i int, remaining int64) bool {
		if remaining == 0 {
			found++
			if found == 1 {
				first = append([]int(nil), pick...)
				return false
			}
			second = append([]int(nil), pick...)
			return true
		}
		if i == len(cand) || remaining < 0 || suffix[i] < remaining {
			return false
		}
		pick = append(pick, cand[i]) // take
		if walk(i+1, remaining-txs[cand[i]].AmountKurus) {
			return true
		}
		pick = pick[:len(pick)-1] // skip
		return walk(i+1, remaining)
	}
	walk(0, target)
	return first, second, found
}
