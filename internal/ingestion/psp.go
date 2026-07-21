package ingestion

import (
	"io"

	"reconpilot/internal/domain"
)

type pspAdapter struct{}

func (pspAdapter) Source() domain.SourceType { return domain.SourcePSP }

func (pspAdapter) Parse(r io.Reader) ([]domain.Transaction, []RejectedRow, error) {
	header := []string{"ref", "counterparty", "amount_kurus", "currency", "occurred_at", "direction"}
	return parseRows(r, header, func(rec []string) (domain.Transaction, error) {
		amount, err := parseKurus(rec[2])
		if err != nil {
			return domain.Transaction{}, err
		}
		at, err := parseDate(rec[4])
		if err != nil {
			return domain.Transaction{}, err
		}
		dir, err := parseDirection(rec[5])
		if err != nil {
			return domain.Transaction{}, err
		}
		return domain.Transaction{
			Source: domain.SourcePSP, ExternalRef: rec[0], CounterpartyRef: rec[1],
			AmountKurus: amount, Currency: rec[3], OccurredAt: at, Direction: dir,
			DedupKey: domain.DedupKey(domain.SourcePSP, rec[0], amount, at, dir),
		}, nil
	})
}
