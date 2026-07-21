package matching

import (
	"sort"

	"reconpilot/internal/domain"
)

const (
	groupWindowDays = 14 // payout period: orders up to 14 days before payout
	groupMemberCap  = 20 // design bound: larger candidate sets fall back
)

// MatchGroups reconciles marketplace payouts against subsets of orders:
// payout.GrossKurus == Σ(order amounts). The subset search is bounded by
// candidate narrowing (counterparty + period window) and groupMemberCap.
// Payouts whose candidate set exceeds the cap are returned as
// groupCandidates and removed from the pool (classified `unknown` later) —
// the design's explicit fallback, so invariants never depend on an
// unbounded search finishing.
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
			// Gross = Σ(credit order amounts): debit lines (refunds) must
			// never be added as positive addends — that would be a false
			// match. Positive-amount credits also guarantee subsetSum's
			// prune preconditions (non-negative addends).
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

// subsetSum finds indices (subset of cand) whose amounts sum to target.
// Deterministic backtracking with a remaining-sum prune. Candidates are
// visited in pool order (chronological FIFO): when several subsets reach
// the target, the one covering the OLDEST orders wins — marketplaces
// settle orders in the order they occurred, and pool order is a total
// order (OccurredAt, ExternalRef, ID), so the result never depends on
// input or map iteration order. The prune requires non-negative amounts,
// which MatchGroups guarantees by filtering candidates. Worst case 2^20
// nodes but the prune keeps realistic settlement data far below that.
func subsetSum(txs []domain.Transaction, cand []int, target int64) ([]int, bool) {
	suffix := make([]int64, len(cand)+1)
	for i := len(cand) - 1; i >= 0; i-- {
		suffix[i] = suffix[i+1] + txs[cand[i]].AmountKurus
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
