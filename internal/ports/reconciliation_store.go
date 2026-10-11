// Package ports defines application boundaries shared by the service's
// driving and driven adapters.
package ports

import (
	"context"

	"reconpilot/internal/classification"
	"reconpilot/internal/domain"
	"reconpilot/internal/matching"
)

// ReconciliationStore is the persistence contract required by the HTTP
// service. Keeping the contract outside both the HTTP and PostgreSQL adapters
// prevents either adapter from owning the other.
type ReconciliationStore interface {
	Ping(context.Context) error
	LoadTransactions(context.Context) ([]domain.Transaction, error)
	SaveResult(context.Context, []matching.Match, []classification.Discrepancy) error
}
