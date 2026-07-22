package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"reconpilot/internal/domain"
	"reconpilot/internal/engine"
	"reconpilot/internal/ingestion"
	"reconpilot/internal/reporting"
	"reconpilot/internal/store"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: recon <ingest|run|report>")
		os.Exit(2)
	}
	switch os.Args[1] {
	case "ingest":
		fs := flag.NewFlagSet("ingest", flag.ExitOnError)
		srcFlag := fs.String("source", "", "psp|bank|marketplace")
		fileFlag := fs.String("file", "", "path to CSV")
		_ = fs.Parse(os.Args[2:])
		ctx := context.Background()
		s, err := store.Open(ctx, os.Getenv("DATABASE_URL"))
		if err != nil {
			fatal(err)
		}
		defer s.Close()
		if err := s.Migrate(ctx); err != nil {
			fatal(err)
		}
		f, err := os.Open(*fileFlag)
		if err != nil {
			fatal(err)
		}
		defer f.Close()
		sum, err := ingestion.Ingest(ctx, s, domain.SourceType(*srcFlag), filepath.Base(*fileFlag), f)
		if err != nil {
			fatal(err)
		}
		fmt.Printf("batch=%d parsed=%d inserted=%d deduped=%d rejected=%d\n",
			sum.BatchID, sum.Parsed, sum.Inserted, sum.Deduped, sum.Rejected)
	case "run":
		ctx := context.Background()
		s, err := store.Open(ctx, os.Getenv("DATABASE_URL"))
		if err != nil {
			fatal(err)
		}
		defer s.Close()
		all, err := s.LoadTransactions(ctx)
		if err != nil {
			fatal(err)
		}
		out, err := engine.Run(all)
		if err != nil {
			fatal(err) // invariant violation → non-zero exit, by design
		}
		if err := s.SaveResult(ctx, out.Matches, out.Discrepancies); err != nil {
			fatal(err)
		}
		fmt.Printf("run complete: tx=%d matches=%d discrepancies=%d\n",
			out.TotalTx, len(out.Matches), len(out.Discrepancies))
	case "report":
		fs := flag.NewFlagSet("report", flag.ExitOnError)
		outDir := fs.String("out", "reports", "output directory")
		_ = fs.Parse(os.Args[2:])
		ctx := context.Background()
		s, err := store.Open(ctx, os.Getenv("DATABASE_URL"))
		if err != nil {
			fatal(err)
		}
		defer s.Close()
		all, err := s.LoadTransactions(ctx)
		if err != nil {
			fatal(err)
		}
		out, err := engine.Run(all)
		if err != nil {
			fatal(err)
		}
		htmlBytes, csvBytes, err := reporting.Render(out, all)
		if err != nil {
			fatal(err)
		}
		if err := os.MkdirAll(*outDir, 0o755); err != nil {
			fatal(err)
		}
		if err := os.WriteFile(filepath.Join(*outDir, "report.html"), htmlBytes, 0o644); err != nil {
			fatal(err)
		}
		if err := os.WriteFile(filepath.Join(*outDir, "discrepancies.csv"), csvBytes, 0o644); err != nil {
			fatal(err)
		}
		fmt.Println("wrote", filepath.Join(*outDir, "report.html"), "and discrepancies.csv")
	default:
		fmt.Fprintln(os.Stderr, "unknown command: "+os.Args[1])
		os.Exit(2)
	}
}

func fatal(err error) { fmt.Fprintln(os.Stderr, "error:", err); os.Exit(1) }
