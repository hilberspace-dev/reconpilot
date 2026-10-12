// Package engine runs the pipeline in order: split the pool, exact, split,
// tolerant and group matching, the tolerant pass again over what is left,
// classification, then the invariant check. It does no
// I/O; the commands connect it to the store.
package engine

import (
	"fmt"
	"math"
	"reconpilot/internal/classification"
	"reconpilot/internal/domain"
	"reconpilot/internal/invariants"
	"reconpilot/internal/matching"
)

type Output struct {
	Matches       []matching.Match
	Discrepancies []classification.Discrepancy
	TotalTx       int
}

func Run(all []domain.Transaction) (Output, error) {
	var total int64
	ids := make(map[int64]bool, len(all))
	for _, tx := range all {
		if err := tx.ValidateMoney(); err != nil {
			return Output{}, fmt.Errorf("transaction %d: %w", tx.ID, err)
		}
		if ids[tx.ID] {
			return Output{}, fmt.Errorf("duplicate transaction ID %d", tx.ID)
		}
		ids[tx.ID] = true
		// Include fees so every report subtotal fits in signed integer minor units.
		for _, amount := range []int64{tx.AmountKurus, tx.CommissionKurus} {
			if amount > math.MaxInt64-total {
				return Output{}, fmt.Errorf("book total exceeds int64 minor units")
			}
			total += amount
		}
	}
	pool := matching.SplitPool(all)
	var matches []matching.Match
	matches = append(matches, matching.MatchExact(&pool)...)
	matches = append(matches, matching.MatchSplits(&pool)...)
	tolerantMatches, _ := matching.MatchTolerant(&pool)
	matches = append(matches, tolerantMatches...)
	groupMatches, groupCandidates, unsureGroups := matching.MatchGroups(&pool)
	matches = append(matches, groupMatches...)
	// The tolerant pass runs once more: an order the group pass took no
	// longer competes for a bank line, and a pair it blocked may now be the
	// only fit. Its contested rows are the ones still open after everything.
	tolerantMatches, contested := matching.MatchTolerant(&pool)
	matches = append(matches, tolerantMatches...)
	open := map[int64]bool{}
	for _, t := range pool.Expected {
		open[t.ID] = true
	}
	for _, t := range pool.Actual {
		open[t.ID] = true
	}
	var ambiguous []int64
	for _, id := range unsureGroups {
		if open[id] {
			ambiguous = append(ambiguous, id)
			open[id] = false // listed once
		}
	}
	for _, id := range contested {
		if open[id] {
			ambiguous = append(ambiguous, id)
			open[id] = false
		}
	}
	discs := classification.Classify(pool, matches, groupCandidates, ambiguous, all)
	if err := invariants.Check(all, matches, discs); err != nil {
		return Output{}, err
	}
	return Output{Matches: matches, Discrepancies: discs, TotalTx: len(all)}, nil
}
