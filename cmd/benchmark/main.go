// Command benchmark generates a seeded synthetic book, runs the engine and
// scores the result against the generator's ground truth. The scoring code
// lives in internal/verification, so this command and the HTML panel report
// the same numbers.
package main

import (
	"flag"
	"fmt"
	"os"

	"reconpilot/internal/verification"
)

func main() {
	n := flag.Int("n", 50000, "approximate transaction count")
	seed := flag.Int64("seed", 1, "generator seed (same seed = same book)")
	flag.Parse()

	res, err := verification.Score(*seed, *n)
	if err != nil {
		fmt.Fprintln(os.Stderr, "INVARIANT FAILURE:", err)
		os.Exit(1)
	}

	fmt.Printf("reconpilot benchmark: n=%d seed=%d\n", res.N, res.Seed)
	fmt.Printf("elapsed: %s\n\n", res.Elapsed)
	fmt.Printf("%-12s %10s %10s %10s %10s\n", "type", "injected", "detected", "emitted", "expected")
	for _, row := range res.Rows {
		mark := "ok"
		switch {
		case row.Short:
			mark = "MISS"
		case row.Surplus:
			mark = "SURPLUS"
		}
		fmt.Printf("%-12s %10d %10d %10d %10d  %s\n", row.Type, row.Injected, row.Detected, row.Emitted, row.Expected, mark)
	}
	fmt.Println("(detected: injected records that got their type. expected: the injected records, a delta-0")
	fmt.Printf(" record for each commission, partial and timing counterpart, and %d group fee records)\n", res.CleanGroups)

	// internal/verification defines what counts as a false or missed match.
	fmt.Printf("\nclean books: pairs=%d unreferenced=%d splits=%d reversals=%d groups=%d, matches produced: %d\n",
		res.CleanPairs, res.Unreferenced, res.Splits, res.Reversals, res.CleanGroups, res.Matches)
	fmt.Printf("false matches: %d, intended pairs/groups not fully matched: %d\n", res.FalseMatches, res.MissedIntended)
	fmt.Printf("runtime invariants: 3/3 PASSED (checked inside engine.Run)\n")
	if _, injected := res.DetectedTypes(); injected == 0 {
		fmt.Println("\nRESULT: FAIL (the book holds no injected discrepancy; use a larger -n)")
		os.Exit(1)
	}
	if !res.Passed() {
		fmt.Println("\nRESULT: FAIL (see the counts above)")
		os.Exit(1)
	}
	detected, injected := res.DetectedTypes()
	fmt.Printf("\nRESULT: PASS, %d/%d injected types detected, 0 false matches, 0 intended pairs/groups missed\n",
		detected, injected)
}
