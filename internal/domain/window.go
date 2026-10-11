package domain

// MatchWindowDays is the largest gap between value dates that the exact and
// tolerant matchers accept. A wider gap is classified as a timing discrepancy.
const MatchWindowDays = 3

// TolerantAmountBP is the tolerant matcher's amount tolerance in basis points
// of the expected amount (50 bp = 0.5 %).
const TolerantAmountBP = 50

// TolerantLimit is the largest amount difference the tolerant matcher accepts
// for an expected amount. Dividing by 200 gives floor(amount*50/10000) for a
// nonnegative amount without forming the product, which could overflow.
func TolerantLimit(amount int64) int64 {
	return amount / (10000 / TolerantAmountBP)
}
