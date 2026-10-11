package domain

import (
	"fmt"
	"math/bits"
)

// ValidateMoney checks the TRY-only input contract before ingestion or matching.
func (t Transaction) ValidateMoney() error {
	if t.Currency != "TRY" {
		return fmt.Errorf("unsupported currency %q: only TRY is supported", t.Currency)
	}
	if t.AmountKurus < 0 || t.GrossKurus < 0 || t.CommissionKurus < 0 {
		return fmt.Errorf("money amounts must be nonnegative; use direction for debits")
	}
	if t.Source == SourceMarketplace && (t.CommissionKurus > t.GrossKurus || t.GrossKurus-t.CommissionKurus != t.AmountKurus) {
		return fmt.Errorf("marketplace gross minus commission must equal net")
	}
	return nil
}

// BasisPoints computes part/whole without overflowing the intermediate product.
// Callers must supply 0 <= part <= whole and whole > 0.
func BasisPoints(part, whole int64) int64 {
	hi, lo := bits.Mul64(uint64(part), 10000)
	q, _ := bits.Div64(hi, lo, uint64(whole))
	return int64(q)
}
