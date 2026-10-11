// Package engine runs the pipeline in order: split the pool, exact, tolerant
// and group matching, classification, then the invariant check. It does no
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
	matches = append(matches, matching.MatchTolerant(&pool)...)
	groupMatches, groupCandidates := matching.MatchGroups(&pool)
	matches = append(matches, groupMatches...)
	discs := classification.Classify(pool, matches, groupCandidates, all)
	if err := invariants.Check(all, matches, discs); err != nil {
		return Output{}, err
	}
	return Output{Matches: matches, Discrepancies: discs, TotalTx: len(all)}, nil
}
