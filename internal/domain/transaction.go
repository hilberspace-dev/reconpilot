// Package domain holds the core reconciliation types. It depends on nothing
// inside this repository.
package domain

import "time"

type SourceType string

const (
	SourcePSP         SourceType = "psp"
	SourceBank        SourceType = "bank"
	SourceMarketplace SourceType = "marketplace"
)

// IsExpected reports whether records from this source represent money we
// expect to receive (PSP ledger) as opposed to money actually received
// (bank statement, marketplace settlement).
func (s SourceType) IsExpected() bool { return s == SourcePSP }

type Direction string

const (
	DirCredit Direction = "credit"
	DirDebit  Direction = "debit"
)

type TxStatus string

const (
	StatusUnmatched      TxStatus = "unmatched"
	StatusMatched        TxStatus = "matched"
	StatusGroupCandidate TxStatus = "group_candidate"
)

type Transaction struct {
	ID              int64
	BatchID         int64
	Source          SourceType
	ExternalRef     string
	CounterpartyRef string // merchant/seller; REQUIRED for group candidate narrowing
	AmountKurus     int64  // integer minor units, never float
	Currency        string
	OccurredAt      time.Time
	Direction       Direction
	DedupKey        string
	Status          TxStatus
	// Marketplace settlement lines carry their own breakdown; zero elsewhere.
	GrossKurus      int64
	CommissionKurus int64
}
