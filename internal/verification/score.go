// Package verification scores an engine run against the generator's ground
// truth. It is the single source of the detection, false-match and
// missed-pair figures, so the numbers printed by the benchmark command and
// the numbers rendered in the verification panel cannot drift apart.
package verification

import (
	"time"

	"reconpilot/internal/engine"
	"reconpilot/internal/generator"
)

// Types is the fixed discrepancy vocabulary, in the order every surface
// reports it. It mirrors the CHECK constraint in the initial migration.
var Types = []string{"commission", "refund", "partial", "timing", "duplicate", "missing", "unknown"}

// inflation explains, per type, why the engine legitimately emits more
// records than the generator injected. A type absent from this map is
// expected to match its injected count exactly.
var inflation = map[string]string{
	"commission": "one fee record per clean marketplace group is also a commission record",
	"partial":    "each injected pair yields the expected-side record plus a delta-0 counterpart mirror",
	"timing":     "each injected pair yields the expected-side record plus a delta-0 counterpart mirror",
}

// TypeRow is the injected-versus-detected comparison for one discrepancy type.
type TypeRow struct {
	Type     string
	Injected int
	Detected int
	// Short is true when detection fell below what was injected. That is the
	// only direction that counts as a failure.
	Short bool
	// Inflation is non-empty when Detected exceeds Injected by design, and
	// carries the reason. Without it a reader sees 77/814 and assumes a bug.
	Inflation string
}

// Result is a scored run. Every figure here is measured, not asserted.
type Result struct {
	N              int
	Seed           int64
	Elapsed        time.Duration
	Rows           []TypeRow
	CleanPairs     int
	CleanGroups    int
	Matches        int
	FalseMatches   int
	MissedIntended int
	Underdetected  bool
}

// Passed reports whether the run satisfies all three published claims.
func (r Result) Passed() bool {
	return !r.Underdetected && r.FalseMatches == 0 && r.MissedIntended == 0
}

// DetectedTypes counts the injected types that were detected at least as
// often as they were injected, over the number of types actually injected.
// This is the "7/7" figure.
func (r Result) DetectedTypes() (detected, injected int) {
	for _, row := range r.Rows {
		if row.Injected == 0 {
			continue
		}
		injected++
		if !row.Short {
			detected++
		}
	}
	return detected, injected
}

// Score generates a seeded synthetic book, runs the engine over it and scores
// the outcome against the generator's ground truth. The returned error is a
// runtime invariant failure from the engine; in that case the run produced no
// output and nothing should be reported.
func Score(seed int64, n int) (Result, error) {
	txs, gt := generator.Generate(seed, n)
	start := time.Now()
	out, err := engine.Run(txs)
	elapsed := time.Since(start)
	if err != nil {
		return Result{}, err
	}

	detected := map[string]int{}
	for _, d := range out.Discrepancies {
		detected[d.Type]++
	}

	res := Result{
		N:           len(txs),
		Seed:        seed,
		Elapsed:     elapsed,
		CleanPairs:  gt.CleanPairs,
		CleanGroups: gt.CleanGroups,
		Matches:     len(out.Matches),
	}
	for _, typ := range Types {
		inj := gt.InjectedByType[typ]
		det := detected[typ]
		row := TypeRow{Type: typ, Injected: inj, Detected: det}
		if inj > 0 && det < inj {
			row.Short = true
			res.Underdetected = true
		}
		if det > inj {
			row.Inflation = inflation[typ]
		}
		res.Rows = append(res.Rows, row)
	}

	// A match is false when its members do not all carry the same intended
	// group label. An intended pair or group is missed when its members are
	// not all matched together. Both are measured, not asserted.
	matchedPerLabel := map[string]int{}
	for _, m := range out.Matches {
		label, ok := gt.IntendedGroup[m.TxIDs[0]]
		clean := ok
		for _, id := range m.TxIDs[1:] {
			if l, ok2 := gt.IntendedGroup[id]; !ok2 || l != label {
				clean = false
			}
		}
		if !clean {
			res.FalseMatches++
			continue
		}
		matchedPerLabel[label] += len(m.TxIDs)
	}
	intendedPerLabel := map[string]int{}
	for _, label := range gt.IntendedGroup {
		intendedPerLabel[label]++
	}
	for label, want := range intendedPerLabel {
		if matchedPerLabel[label] != want {
			res.MissedIntended++
		}
	}
	return res, nil
}
