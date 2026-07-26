// Verify: the same reproducible proof the benchmark prints, rendered as a
// self-contained HTML panel so it can be read in a browser instead of a
// terminal. Needs no database and no prior ingest — it generates the book,
// runs the engine and scores the result in one process.
package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"

	"reconpilot/internal/verification"
)

func main() {
	n := flag.Int("n", 50000, "approximate transaction count")
	seed := flag.Int64("seed", 1, "generator seed (same seed = same book)")
	out := flag.String("out", "", "write the panel to this path (default verification.html)")
	listen := flag.String("listen", "", "serve the panel on this address instead of writing a file, e.g. :8081")
	flag.Parse()

	res, err := verification.Score(*seed, *n)
	if err != nil {
		fmt.Fprintln(os.Stderr, "INVARIANT FAILURE:", err)
		os.Exit(1)
	}
	html, err := verification.RenderHTML(res)
	if err != nil {
		fmt.Fprintln(os.Stderr, "render failed:", err)
		os.Exit(1)
	}

	detected, injected := res.DetectedTypes()
	verdict := "PASS"
	if !res.Passed() {
		verdict = "FAIL"
	}
	summary := fmt.Sprintf("%s — %d/%d injected types detected, %d false matches, %d intended pairs/groups missed (n=%d seed=%d)",
		verdict, detected, injected, res.FalseMatches, res.MissedIntended, res.N, res.Seed)

	if *listen != "" {
		http.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			fmt.Fprintln(w, "ok")
		})
		http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/" {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("X-Content-Type-Options", "nosniff")
			if _, err := w.Write(html); err != nil {
				return
			}
		})
		fmt.Printf("verification panel on %s — %s\n", *listen, summary)
		if err := http.ListenAndServe(*listen, nil); err != nil {
			fmt.Fprintln(os.Stderr, "serve failed:", err)
			os.Exit(1)
		}
		return
	}

	path := *out
	if path == "" {
		path = "verification.html"
	}
	if err := os.WriteFile(path, html, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "write failed:", err)
		os.Exit(1)
	}
	fmt.Printf("wrote %s — %s\n", path, summary)
	if !res.Passed() {
		os.Exit(1)
	}
}
