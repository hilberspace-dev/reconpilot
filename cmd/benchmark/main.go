// Benchmark: the reproducible proof behind the README claim (design §2).
// Generates a seeded synthetic book, runs the engine, and scores detection
// against the generator's ground truth. Scoring lives in
// internal/verification so this command and the HTML verification panel
// report the same numbers.
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

	fmt.Printf("reconpilot benchmark — n=%d seed=%d\n", res.N, res.Seed)
	fmt.Printf("elapsed: %s\n\n", res.Elapsed)
	fmt.Printf("%-12s %10s %10s\n", "type", "injected", "detected")
	for _, row := range res.Rows {
		mark := "ok"
		if row.Short {
			mark = "MISS"
		}
		fmt.Printf("%-12s %10d %10d  %s\n", row.Type, row.Injected, row.Detected, mark)
	}
	fmt.Printf("(commission 'detected' includes one fee record per clean marketplace group: %d groups)\n", res.CleanGroups)

	// False matches and missed intended groups are MEASURED, not asserted;
	// see internal/verification for the scoring rules.
	fmt.Printf("\nclean books: pairs=%d groups=%d — matches produced: %d\n",
		res.CleanPairs, res.CleanGroups, res.Matches)
	fmt.Printf("false matches: %d — intended pairs/groups not fully matched: %d\n", res.FalseMatches, res.MissedIntended)
	fmt.Printf("runtime invariants: 3/3 PASSED (checked inside engine.Run)\n")
	if !res.Passed() {
		fmt.Println("\nRESULT: FAIL — detection or match integrity fell short (see above)")
		os.Exit(1)
	}
	fmt.Println("\nRESULT: PASS — 7/7 injected types detected, 0 false matches, 0 intended pairs/groups missed")
}
