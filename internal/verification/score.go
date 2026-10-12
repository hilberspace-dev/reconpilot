// Package verification compares engine output with seeded ground truth
// for both the CLI benchmark and the verification panel.
package verification

import (
	"time"

	"reconpilot/internal/engine"
	"reconpilot/internal/generator"
)

// Types is the fixed discrepancy vocabulary, in the order every surface
// reports it. It mirrors the CHECK constraint in the store migrations.
var Types = []string{"commission", "refund", "partial", "timing", "duplicate", "missing", "ambiguous", "unknown"}

// mirrored lists the types whose injected pairs also produce a delta-0 record
// for the counterpart row on the actual side.
var mirrored = map[string]bool{"commission": true, "partial": true, "timing": true}

// TypeRow compares, for one discrepancy type, what was injected with what the
// engine produced. Detected counts the injected records that got this type;
// Emitted counts every record of the type, and Expected is the number the book
// accounts for (injected records, a delta-0 record for each commission,
// partial and timing counterpart, and one fee record per clean group).
type TypeRow struct {
	Type     string
	Injected int
	Detected int
	Emitted  int
	Expected int
	Short    bool   // an injected record did not get this type
	Surplus  bool   // Emitted differs from Expected
	Note     string // why Expected is above Injected, when it is
}

// Result holds the scores of one run.
type Result struct {
	N              int
	Seed           int64
	Elapsed        time.Duration
	Rows           []TypeRow
	CleanPairs     int
	CleanGroups    int
	Unreferenced   int
	Splits         int
	Reversals      int
	Matches        int
	FalseMatches   int
	MissedIntended int
	Underdetected  bool
	Unexplained    bool
}

// Passed reports whether at least one type was injected, every injected
// record got its type, every record the engine produced is accounted for, no
// match is false and every intended pair or group was matched whole.
func (r Result) Passed() bool {
	_, injected := r.DetectedTypes()
	return injected > 0 && !r.Underdetected && !r.Unexplained && r.FalseMatches == 0 && r.MissedIntended == 0
}

// DetectedTypes counts the injected types whose every injected record got
// that type, over the number of types actually injected. This is the "7/7"
// figure.
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
	res := tally(gt, out)
	res.N, res.Seed, res.Elapsed = len(txs), seed, elapsed
	return res, nil
}

// tally scores engine output against the ground truth.
func tally(gt generator.GroundTruth, out engine.Output) Result {
	res := Result{CleanPairs: gt.CleanPairs, CleanGroups: gt.CleanGroups, Unreferenced: gt.UnreferencedPairs,
		Splits: gt.CleanSplits, Reversals: gt.CleanReversals, Matches: len(out.Matches)}

	emitted := map[string]int{}
	typeOf := map[int64]string{}
	for _, d := range out.Discrepancies {
		emitted[d.Type]++
		if d.TxID != nil {
			typeOf[*d.TxID] = d.Type
		}
	}
	detected := map[string]int{}
	for id, typ := range gt.InjectedTx {
		if typeOf[id] == typ {
			detected[typ]++
		}
	}
	for _, typ := range Types {
		inj := gt.InjectedByType[typ]
		row := TypeRow{Type: typ, Injected: inj, Detected: detected[typ], Emitted: emitted[typ], Expected: inj}
		if mirrored[typ] && inj > 0 {
			row.Expected += inj
			row.Note = "each injected pair also gives a delta-0 record for its counterpart"
		}
		if typ == "commission" && gt.CleanGroups > 0 {
			row.Expected += gt.CleanGroups
			if row.Note != "" {
				row.Note += ", and "
			}
			row.Note += "each clean marketplace group gives one fee record"
		}
		row.Short = row.Detected < inj
		row.Surplus = row.Emitted != row.Expected
		res.Underdetected = res.Underdetected || row.Short
		res.Unexplained = res.Unexplained || row.Surplus
		res.Rows = append(res.Rows, row)
	}

	// A match is false when its members do not all carry the same intended
	// label. An intended pair or group is missed unless one match holds all
	// of its members.
	intended := map[string]int{}
	for _, label := range gt.IntendedGroup {
		intended[label]++
	}
	whole := map[string]int{}
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
		if len(m.TxIDs) == intended[label] {
			whole[label]++
		}
	}
	for label := range intended {
		if whole[label] != 1 {
			res.MissedIntended++
		}
	}
	return res
}
