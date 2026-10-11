package verification

import (
	"bytes"
	"testing"

	"reconpilot/internal/classification"
	"reconpilot/internal/engine"
	"reconpilot/internal/generator"
	"reconpilot/internal/matching"
)

// A scored run must reproduce the published claims: every injected record
// gets its type, every emitted record is accounted for, no false match, and
// no intended pair or group left partially matched.
func TestScoreMeetsPublishedClaims(t *testing.T) {
	res, err := Score(1, 4000)
	if err != nil {
		t.Fatalf("invariant failure: %v", err)
	}
	if res.N == 0 {
		t.Fatal("no transactions generated")
	}
	detected, injected := res.DetectedTypes()
	if injected != len(Types) {
		t.Fatalf("expected all %d types to be injected, got %d", len(Types), injected)
	}
	if detected != injected {
		t.Errorf("under-detection: %d/%d types", detected, injected)
		for _, row := range res.Rows {
			if row.Short {
				t.Errorf("  %s: injected %d, detected %d", row.Type, row.Injected, row.Detected)
			}
		}
	}
	if res.FalseMatches != 0 {
		t.Errorf("false matches: %d, want 0", res.FalseMatches)
	}
	if res.MissedIntended != 0 {
		t.Errorf("intended pairs/groups not fully matched: %d, want 0", res.MissedIntended)
	}
	if !res.Passed() {
		t.Error("Passed() reported false on a run with no shortfall")
	}
}

// The same seed gives the same book and the same numbers. The panel relies on
// this when it asks a reader to reproduce the result.
func TestScoreIsDeterministic(t *testing.T) {
	a, err := Score(7, 3000)
	if err != nil {
		t.Fatalf("first run: %v", err)
	}
	b, err := Score(7, 3000)
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if a.N != b.N || a.Matches != b.Matches || a.FalseMatches != b.FalseMatches ||
		a.MissedIntended != b.MissedIntended || a.CleanPairs != b.CleanPairs || a.CleanGroups != b.CleanGroups {
		t.Fatalf("same seed produced different figures:\n%+v\n%+v", a, b)
	}
	for i := range a.Rows {
		if a.Rows[i] != b.Rows[i] {
			t.Fatalf("row %d differs: %+v vs %+v", i, a.Rows[i], b.Rows[i])
		}
	}
}

// Passed() must fail on each condition independently, so a regression in one
// cannot be masked by the others.
func TestPassedFailsOnEachCondition(t *testing.T) {
	rows := []TypeRow{{Type: "missing", Injected: 1, Detected: 1, Emitted: 1, Expected: 1}}
	for _, tc := range []struct {
		name string
		res  Result
	}{
		{"under-detected", Result{Rows: rows, Underdetected: true}},
		{"unexplained records", Result{Rows: rows, Unexplained: true}},
		{"false match", Result{Rows: rows, FalseMatches: 1}},
		{"missed intended", Result{Rows: rows, MissedIntended: 1}},
		{"nothing injected", Result{}},
	} {
		if tc.res.Passed() {
			t.Errorf("%s: Passed() returned true", tc.name)
		}
	}
	if !(Result{Rows: rows}).Passed() {
		t.Error("a clean result should pass")
	}
}

// The panel must state the measured verdict and carry no external reference,
// because it is served from a scratch container and read from file:// URLs.
func TestRenderHTMLIsSelfContained(t *testing.T) {
	res, err := Score(1, 2000)
	if err != nil {
		t.Fatalf("score: %v", err)
	}
	html, err := RenderHTML(res)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !bytes.Contains(html, []byte("PASS")) {
		t.Error("verdict missing from the rendered panel")
	}
	for _, typ := range Types {
		if !bytes.Contains(html, []byte(typ)) {
			t.Errorf("discrepancy type %q missing from the panel", typ)
		}
	}
	for _, forbidden := range []string{"http://", "https://", "<script"} {
		if bytes.Contains(html, []byte(forbidden)) {
			t.Errorf("panel is not self-contained: found %q", forbidden)
		}
	}
}

// The published claim must not depend on the default seed. Seed 42 is the
// one the README tells readers to try.
func TestScoreHoldsAcrossSeeds(t *testing.T) {
	if testing.Short() {
		t.Skip("31 full-size runs; skipped with -short")
	}
	seeds := []int64{42}
	for s := int64(1); s <= 30; s++ {
		seeds = append(seeds, s)
	}
	for _, seed := range seeds {
		res, err := Score(seed, 50000)
		if err != nil {
			t.Fatalf("seed %d: invariant failure: %v", seed, err)
		}
		if !res.Passed() {
			t.Errorf("seed %d: false matches %d, intended pairs/groups missed %d, underdetected %v, unexplained %v",
				seed, res.FalseMatches, res.MissedIntended, res.Underdetected, res.Unexplained)
		}
	}
}

func ptr(id int64) *int64 { return &id }

// A count can hide a record that got the wrong type: here the commission
// count is right, but the injected record 1 was called partial.
func TestTallyChecksEachInjectedRecord(t *testing.T) {
	gt := generator.GroundTruth{
		InjectedByType: map[string]int{"commission": 1},
		InjectedTx:     map[int64]string{1: "commission"},
	}
	out := engine.Output{Discrepancies: []classification.Discrepancy{
		{TxID: ptr(1), Type: "partial"},
		{TxID: ptr(2), Type: "commission"},
		{TxID: ptr(3), Type: "commission"},
	}}
	res := tally(gt, out)
	if !res.Underdetected || res.Rows[0].Detected != 0 || res.Passed() {
		t.Fatalf("misclassified commission not caught: %+v", res.Rows[0])
	}
}

func TestTallyFlagsUnexplainedRecords(t *testing.T) {
	gt := generator.GroundTruth{
		InjectedByType: map[string]int{"missing": 1},
		InjectedTx:     map[int64]string{1: "missing"},
	}
	out := engine.Output{Discrepancies: []classification.Discrepancy{
		{TxID: ptr(1), Type: "missing"},
		{TxID: ptr(9), Type: "missing"},
	}}
	res := tally(gt, out)
	if res.Underdetected || !res.Unexplained || res.Passed() {
		t.Fatalf("surplus missing record not caught: %+v", res)
	}
}

// Two clean matches that together hold a group are not the group matched
// whole.
func TestTallyRequiresOneMatchPerIntendedGroup(t *testing.T) {
	gt := generator.GroundTruth{IntendedGroup: map[int64]string{1: "PAY-1", 2: "PAY-1", 3: "PAY-1"}}
	out := engine.Output{Matches: []matching.Match{{TxIDs: []int64{1, 2}}, {TxIDs: []int64{3}}}}
	if res := tally(gt, out); res.MissedIntended != 1 || res.FalseMatches != 0 {
		t.Fatalf("split group: missed %d, false %d", res.MissedIntended, res.FalseMatches)
	}
	whole := engine.Output{Matches: []matching.Match{{TxIDs: []int64{1, 2, 3}}}}
	if res := tally(gt, whole); res.MissedIntended != 0 {
		t.Fatalf("whole group counted as missed: %d", res.MissedIntended)
	}
}
