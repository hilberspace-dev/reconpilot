// Package ingestion parses source CSVs into domain transactions. A row that
// cannot be parsed never aborts the batch: it becomes a RejectedRow
// (poison-record rule from the design).
package ingestion

import (
	"encoding/csv"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"reconpilot/internal/domain"
)

type RejectedRow struct {
	RowNo  int
	Reason string
	Raw    string
}

type Adapter interface {
	Source() domain.SourceType
	Parse(r io.Reader) ([]domain.Transaction, []RejectedRow, error)
}

func AdapterFor(src domain.SourceType) (Adapter, error) {
	switch src {
	case domain.SourcePSP:
		return pspAdapter{}, nil
	case domain.SourceBank:
		return bankAdapter{}, nil
	case domain.SourceMarketplace:
		return marketplaceAdapter{}, nil
	}
	return nil, fmt.Errorf("no adapter for source %q", src)
}

const dateLayout = "2006-01-02"

// parseRows drives the shared CSV loop; perRow converts one record or errors.
func parseRows(r io.Reader, wantHeader []string, perRow func(rec []string) (domain.Transaction, error)) ([]domain.Transaction, []RejectedRow, error) {
	cr := csv.NewReader(r)
	header, err := cr.Read()
	if err != nil {
		return nil, nil, fmt.Errorf("reading header: %w", err)
	}
	if strings.Join(header, ",") != strings.Join(wantHeader, ",") {
		return nil, nil, fmt.Errorf("unexpected header %v, want %v", header, wantHeader)
	}
	var txs []domain.Transaction
	var rejected []RejectedRow
	rowNo := 1
	for {
		rec, err := cr.Read()
		if err == io.EOF {
			break
		}
		rowNo++
		if err != nil {
			rejected = append(rejected, RejectedRow{rowNo, err.Error(), ""})
			continue
		}
		tx, err := perRow(rec)
		if err != nil {
			rejected = append(rejected, RejectedRow{rowNo, err.Error(), strings.Join(rec, ",")})
			continue
		}
		tx.Status = domain.StatusUnmatched
		txs = append(txs, tx)
	}
	return txs, rejected, nil
}

func parseKurus(s string) (int64, error) {
	v, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("amount %q is not integer kurus: %w", s, err)
	}
	return v, nil
}

func parseDate(s string) (time.Time, error) {
	return time.ParseInLocation(dateLayout, strings.TrimSpace(s), time.UTC)
}

func parseDirection(s string) (domain.Direction, error) {
	switch domain.Direction(s) {
	case domain.DirCredit, domain.DirDebit:
		return domain.Direction(s), nil
	}
	return "", fmt.Errorf("bad direction %q", s)
}
