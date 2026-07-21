package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"reconpilot/internal/domain"
	"reconpilot/internal/ingestion"
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
	case "run", "report":
		fmt.Fprintln(os.Stderr, os.Args[1]+": not implemented yet")
		os.Exit(1)
	default:
		fmt.Fprintln(os.Stderr, "unknown command: "+os.Args[1])
		os.Exit(2)
	}
}

func fatal(err error) { fmt.Fprintln(os.Stderr, "error:", err); os.Exit(1) }
