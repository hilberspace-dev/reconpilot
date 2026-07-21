// Package engine composes the pipeline: split → exact → tolerant → group →
// classify → invariant check. It owns no I/O; cmd wires it to the store.
package engine

import (
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
