package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"reconpilot/internal/domain"
	"reconpilot/internal/engine"
	"reconpilot/internal/ingestion"
	"reconpilot/internal/reporting"
	httpserver "reconpilot/internal/server"
	"reconpilot/internal/store"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: recon <ingest|run|report|demo|serve|healthcheck>")
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
	case "demo":
		if err := demo(os.Args[2:]); err != nil {
			fatal(err)
		}
	case "serve":
		if err := serve(os.Args[2:]); err != nil {
			fatal(err)
		}
	case "healthcheck":
		if err := healthcheck(os.Args[2:]); err != nil {
			fatal(err)
		}
	default:
		fmt.Fprintln(os.Stderr, "unknown command: "+os.Args[1])
		os.Exit(2)
	}
}

func demo(args []string) error {
	fs := flag.NewFlagSet("demo", flag.ContinueOnError)
	dir := fs.String("dir", "testdata/golden", "directory containing the three golden CSV files")
	if err := fs.Parse(args); err != nil {
		return err
	}

	ctx := context.Background()
	s, err := store.Open(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer s.Close()
	if err := s.Migrate(ctx); err != nil {
		return fmt.Errorf("migrate store: %w", err)
	}

	inputs := []struct {
		source domain.SourceType
		name   string
	}{
		{domain.SourcePSP, "psp.csv"},
		{domain.SourceBank, "bank.csv"},
		{domain.SourceMarketplace, "marketplace.csv"},
	}
	for _, input := range inputs {
		path := filepath.Join(*dir, input.name)
		f, err := os.Open(path)
		if err != nil {
			return fmt.Errorf("open %s: %w", path, err)
		}
		summary, ingestErr := ingestion.Ingest(ctx, s, input.source, input.name, f)
		closeErr := f.Close()
		if ingestErr != nil {
			return fmt.Errorf("ingest %s: %w", input.name, ingestErr)
		}
		if closeErr != nil {
			return fmt.Errorf("close %s: %w", input.name, closeErr)
		}
		fmt.Printf("seed %s: parsed=%d inserted=%d deduped=%d rejected=%d\n",
			input.name, summary.Parsed, summary.Inserted, summary.Deduped, summary.Rejected)
	}

	all, err := s.LoadTransactions(ctx)
	if err != nil {
		return fmt.Errorf("load transactions: %w", err)
	}
	out, err := engine.Run(all)
	if err != nil {
		return fmt.Errorf("run reconciliation: %w", err)
	}
	if err := s.SaveResult(ctx, out.Matches, out.Discrepancies); err != nil {
		return fmt.Errorf("persist reconciliation: %w", err)
	}
	fmt.Printf("demo ready: tx=%d matches=%d discrepancies=%d\n",
		out.TotalTx, len(out.Matches), len(out.Discrepancies))
	return nil
}

func healthcheck(args []string) error {
	fs := flag.NewFlagSet("healthcheck", flag.ContinueOnError)
	url := fs.String("url", "http://127.0.0.1:8080/readyz", "readiness URL")
	timeout := fs.Duration("timeout", 2*time.Second, "request timeout")
	if err := fs.Parse(args); err != nil {
		return err
	}
	client := &http.Client{Timeout: *timeout}
	resp, err := client.Get(*url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("readiness returned HTTP %d", resp.StatusCode)
	}
	return nil
}

func serve(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	listen := fs.String("listen", ":8080", "HTTP listen address")
	shutdownTimeout := fs.Duration("shutdown-timeout", 10*time.Second, "graceful shutdown timeout")
	if err := fs.Parse(args); err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	s, err := store.Open(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer s.Close()
	if err := s.Migrate(ctx); err != nil {
		return fmt.Errorf("migrate store: %w", err)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	httpServer := &http.Server{
		Addr:              *listen,
		Handler:           httpserver.New(s, logger),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() {
		logger.Info("ReconPilot HTTP service listening", "address", *listen)
		errCh <- httpServer.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), *shutdownTimeout)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown HTTP service: %w", err)
		}
		err := <-errCh
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}
}

func fatal(err error) { fmt.Fprintln(os.Stderr, "error:", err); os.Exit(1) }
