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
	type txDisc struct {
		typ   string
		delta int64
	}
	byTx := map[int64][]txDisc{}
	for _, d := range discs {
		if d.TxID != nil {
			byTx[*d.TxID] = append(byTx[*d.TxID], txDisc{d.Type, d.DeltaKurus})
		}
	}
	for _, t := range all {
		_, matched := seen[t.ID]
		ds, hasDisc := byTx[t.ID]
		if !matched && !hasDisc {
			return fmt.Errorf("invariant 1 violated: transaction %d (%d kurus) is neither matched nor classified", t.ID, t.AmountKurus)
		}
		if matched {
			continue
		}
		// Type-aware form of invariant 3: every transaction-level delta must
		// obey its type's money semantics relative to the transaction's own
		// amount.
		for _, d := range ds {
			ok := false
			switch d.typ {
			case "timing":
				ok = d.delta == 0 // shifted money is not missing money
			case "partial", "commission":
				// Shortfall strictly below the amount; 0 marks the delta-0
				// counterpart mirror on the actual side.
				ok = d.delta >= 0 && d.delta < t.AmountKurus
			case "refund":
				ok = d.delta == -t.AmountKurus // money moved back, full amount
			case "missing", "duplicate", "unknown":
				ok = d.delta == t.AmountKurus // the whole amount is unexplained
			}
			if !ok {
				return fmt.Errorf("invariant 3 violated: tx %d amount %d classified %s with delta %d",
					t.ID, t.AmountKurus, d.typ, d.delta)
			}
		}
	}
	return nil
}
