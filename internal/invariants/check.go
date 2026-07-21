// Package invariants is the cross-cutting proof layer (design §3). Check is
// called after every engine run; a non-nil error aborts the run with a
// non-zero exit. Violations are never downgraded to warnings.
package invariants

import (
	"fmt"

	"reconpilot/internal/classification"
	"reconpilot/internal/domain"
	"reconpilot/internal/matching"
)

func Check(all []domain.Transaction, matches []matching.Match, discs []classification.Discrepancy) error {
	seen := map[int64]int{}
	for _, m := range matches {
		for _, id := range m.TxIDs {
			seen[id]++
			if seen[id] > 1 {
				return fmt.Errorf("invariant 2 violated: transaction %d in more than one match group", id)
			}
		}
	}
	classified := map[int64]int64{} // txID → Σ|delta|
	for _, d := range discs {
		if d.TxID != nil {
			classified[*d.TxID] += abs(d.DeltaKurus)
		}
	}
	for _, t := range all {
		_, matched := seen[t.ID]
		delta, hasDisc := classified[t.ID]
		if !matched && !hasDisc {
			return fmt.Errorf("invariant 1 violated: transaction %d (%d kurus) is neither matched nor classified", t.ID, t.AmountKurus)
		}
		if !matched && hasDisc {
			// Per-transaction form of invariant 3: a classified delta is
			// either 0 (timing shifts and counterpart mirrors move no money)
			// or exactly the transaction's own amount. Summed over all
			// transactions this yields the report's total-difference equality.
			if delta != 0 && delta != t.AmountKurus {
				return fmt.Errorf("invariant 3 violated: tx %d amount %d but classified delta %d", t.ID, t.AmountKurus, delta)
			}
		}
	}
	return nil
}

func abs(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}
