package engine_test

import (
	"fmt"
	"testing"
	"time"

	"pgregory.net/rapid"
	"reconpilot/internal/domain"
	"reconpilot/internal/engine"
)

// Property: for ANY combination of paired, shifted, missing and noise
// transactions, engine.Run either errors or places every transaction —
// invariants.Check inside Run is the oracle. rapid shrinks any failure to
// a minimal counterexample.
func TestEngineInvariantsHoldForArbitraryInput(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		var all []domain.Transaction
		id := int64(0)
		nextID := func() int64 { id++; return id }
		day0 := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)

		nPairs := rapid.IntRange(0, 30).Draw(rt, "pairs")
		for i := 0; i < nPairs; i++ {
			amount := rapid.Int64Range(1, 5_000_00).Draw(rt, fmt.Sprintf("amt%d", i))
			day := rapid.IntRange(0, 20).Draw(rt, fmt.Sprintf("day%d", i))
			shift := rapid.IntRange(0, 6).Draw(rt, fmt.Sprintf("shift%d", i))
			ref := fmt.Sprintf("ORD-%d", i)
			all = append(all, domain.Transaction{ID: nextID(), Source: domain.SourcePSP,
				ExternalRef: ref, CounterpartyRef: "s1", AmountKurus: amount, Currency: "TRY",
				OccurredAt: day0.AddDate(0, 0, day), Direction: domain.DirCredit})
			all = append(all, domain.Transaction{ID: nextID(), Source: domain.SourceBank,
				ExternalRef: ref, CounterpartyRef: "s1", AmountKurus: amount, Currency: "TRY",
				OccurredAt: day0.AddDate(0, 0, day+shift), Direction: domain.DirCredit})
		}
		nLoners := rapid.IntRange(0, 15).Draw(rt, "loners")
		for i := 0; i < nLoners; i++ {
			amount := rapid.Int64Range(1, 5_000_00).Draw(rt, fmt.Sprintf("lamt%d", i))
			src := domain.SourcePSP
			if rapid.Bool().Draw(rt, fmt.Sprintf("side%d", i)) {
				src = domain.SourceBank
			}
			all = append(all, domain.Transaction{ID: nextID(), Source: src,
				ExternalRef: fmt.Sprintf("LON-%d", i), CounterpartyRef: "s1",
				AmountKurus: amount, Currency: "TRY",
				OccurredAt: day0, Direction: domain.DirCredit})
		}
		if _, err := engine.Run(all); err != nil {
			rt.Fatalf("invariant violated on generated input: %v", err)
		}
	})
}
