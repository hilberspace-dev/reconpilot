package matching

import (
	"math"
	"sort"

	"reconpilot/internal/domain"
)

const (
	groupWindowDays = 14 // payout period: orders up to 14 days before payout
	groupMemberCap  = 20 // larger candidate sets are classified as unknown (ADR 0003)
)

// MatchGroups reconciles marketplace payouts against groups of orders.
// Searches above groupMemberCap return an explicit unknown classification.
func MatchGroups(p *Pool) (matches []Match, groupCandidates []int64) {
	var usedAct []int
	claimedExp := map[int]bool{}
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
		lo := payout.OccurredAt.AddDate(0, 0, -groupWindowDays)
		var cand []int // indices into p.Expected
		for ei, e := range p.Expected {
			if claimedExp[ei] || e.CounterpartyRef != payout.CounterpartyRef {
				continue
			}
			// The gross is the sum of credit order amounts. Adding a debit
			// (a refund) as a positive amount would make a false match, and
			// subsetSum's pruning assumes positive amounts anyway.
			if e.Direction != domain.DirCredit || e.AmountKurus <= 0 {
				continue
			}
			if e.OccurredAt.Before(lo) || e.OccurredAt.After(payout.OccurredAt) {
				continue
			}
			cand = append(cand, ei)
		}
		if len(cand) > groupMemberCap {
			groupCandidates = append(groupCandidates, payout.ID)
			usedAct = append(usedAct, ai)
			continue
		}
		subset, ok := subsetSum(p.Expected, cand, payout.GrossKurus)
		if !ok {
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
	return matches, groupCandidates
}

// subsetSum finds the first subset equal to target, preferring older
// transactions. The caller filters out non-positive amounts. Search is
// bounded by groupMemberCap (worst case 2^20 combinations).
func subsetSum(txs []domain.Transaction, cand []int, target int64) ([]int, bool) {
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
	var walk func(i int, remaining int64) bool
	walk = func(i int, remaining int64) bool {
		if remaining == 0 {
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
	if walk(0, target) {
		return pick, true
	}
	return nil, false
}
