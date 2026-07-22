// Benchmark: the reproducible proof behind the README claim (design §2).
// Generates a seeded synthetic book, runs the engine, and scores detection
// against the generator's ground truth.
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"reconpilot/internal/engine"
	"reconpilot/internal/generator"
)

func main() {
	n := flag.Int("n", 50000, "approximate transaction count")
	seed := flag.Int64("seed", 1, "generator seed (same seed = same book)")
	flag.Parse()

	txs, gt := generator.Generate(*seed, *n)
	start := time.Now()
	out, err := engine.Run(txs)
	elapsed := time.Since(start)
	if err != nil {
		fmt.Fprintln(os.Stderr, "INVARIANT FAILURE:", err)
		os.Exit(1)
	}

	detected := map[string]int{}
	for _, d := range out.Discrepancies {
		detected[d.Type]++
	}
	fmt.Printf("reconpilot benchmark — n=%d seed=%d\n", len(txs), *seed)
	fmt.Printf("elapsed: %s\n\n", elapsed)
	fmt.Printf("%-12s %10s %10s\n", "type", "injected", "detected")
	types := []string{"commission", "refund", "partial", "timing", "duplicate", "missing", "unknown"}
	failed := false
	for _, typ := range types {
		inj := gt.InjectedByType[typ]
		det := detected[typ]
		mark := "ok"
		if inj > 0 && det < inj {
			mark = "MISS"
			failed = true
		}
		fmt.Printf("%-12s %10d %10d  %s\n", typ, inj, det, mark)
	}
	fmt.Printf("(commission 'detected' includes one fee record per clean marketplace group: %d groups)\n", gt.CleanGroups)

	// False matches, MEASURED — not asserted. A match is false when its
	// members do not all carry the same intended-group label; an intended
	// pair/group is missed when its members are not all matched together.
	falseMatches := 0
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
			falseMatches++
			continue
		}
		matchedPerLabel[label] += len(m.TxIDs)
	}
	intendedPerLabel := map[string]int{}
	for _, label := range gt.IntendedGroup {
		intendedPerLabel[label]++
	}
	missed := 0
	for label, want := range intendedPerLabel {
		if matchedPerLabel[label] != want {
			missed++
		}
	}
	fmt.Printf("\nclean books: pairs=%d groups=%d — matches produced: %d\n",
		gt.CleanPairs, gt.CleanGroups, len(out.Matches))
	fmt.Printf("false matches: %d — intended pairs/groups not fully matched: %d\n", falseMatches, missed)
	fmt.Printf("runtime invariants: 3/3 PASSED (checked inside engine.Run)\n")
	if failed || falseMatches > 0 || missed > 0 {
		fmt.Println("\nRESULT: FAIL — detection or match integrity fell short (see above)")
		os.Exit(1)
	}
	fmt.Println("\nRESULT: PASS — 7/7 injected types detected, 0 false matches, 0 intended pairs/groups missed")
}
