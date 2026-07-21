package ingestion

import (
	"io"

	"reconpilot/internal/domain"
)

type bankAdapter struct{}

func (bankAdapter) Source() domain.SourceType { return domain.SourceBank }

func (bankAdapter) Parse(r io.Reader) ([]domain.Transaction, []RejectedRow, error) {
	header := []string{"stmt_ref", "counterparty", "amount_kurus", "currency", "value_date", "direction"}
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
			Source: domain.SourceBank, ExternalRef: rec[0], CounterpartyRef: rec[1],
			AmountKurus: amount, Currency: rec[3], OccurredAt: at, Direction: dir,
			DedupKey: domain.DedupKey(domain.SourceBank, rec[0], amount, at, dir),
		}, nil
	})
}
