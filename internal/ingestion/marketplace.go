package ingestion

import (
	"fmt"
	"io"

	"reconpilot/internal/domain"
)

type marketplaceAdapter struct{}

func (marketplaceAdapter) Source() domain.SourceType { return domain.SourceMarketplace }

func (marketplaceAdapter) Parse(r io.Reader) ([]domain.Transaction, []RejectedRow, error) {
	header := []string{"payout_ref", "counterparty", "gross_kurus", "commission_kurus", "net_kurus", "payout_date"}
	return parseRows(r, header, func(rec []string) (domain.Transaction, error) {
		gross, err := parseKurus(rec[2])
		if err != nil {
			return domain.Transaction{}, err
		}
		commission, err := parseKurus(rec[3])
		if err != nil {
			return domain.Transaction{}, err
		}
		net, err := parseKurus(rec[4])
		if err != nil {
			return domain.Transaction{}, err
		}
		if gross-commission != net {
			return domain.Transaction{}, fmt.Errorf("breakdown mismatch: gross %d - commission %d != net %d", gross, commission, net)
		}
		at, err := parseDate(rec[5])
		if err != nil {
			return domain.Transaction{}, err
		}
		return domain.Transaction{
			Source: domain.SourceMarketplace, ExternalRef: rec[0], CounterpartyRef: rec[1],
			AmountKurus: net, GrossKurus: gross, CommissionKurus: commission,
			Currency: "TRY", OccurredAt: at, Direction: domain.DirCredit,
			DedupKey: domain.DedupKey(domain.SourceMarketplace, rec[0], net, at, domain.DirCredit),
		}, nil
	})
}
