package verification

import (
	"bytes"
	"testing"
)

// A scored run must reproduce the published claims: every injected type
// detected at least as often as it was injected, no false match, and no
// intended pair or group left partially matched.
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

// Same seed, same book, same numbers — the property the panel relies on when
// it invites a reader to reproduce the result.
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

// Passed() must fail on each of the three conditions independently, so a
// regression in one cannot be masked by the other two.
func TestPassedFailsOnEachCondition(t *testing.T) {
	for _, tc := range []struct {
		name string
		res  Result
	}{
		{"under-detected", Result{Underdetected: true}},
		{"false match", Result{FalseMatches: 1}},
		{"missed intended", Result{MissedIntended: 1}},
	} {
		if tc.res.Passed() {
			t.Errorf("%s: Passed() returned true", tc.name)
		}
	}
	if !(Result{}).Passed() {
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
