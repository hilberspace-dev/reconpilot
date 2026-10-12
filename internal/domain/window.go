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

// AmountHorizonDays is how far apart two rows of one counterparty and amount
// can be and still be taken for the same money when no reference ties them.
// Beyond the match window such a pair is a timing difference, and its rows
// stop a same-amount pair inside the window from counting as the only fit: a
// check that clears eight days late leaves its own clearance outside the
// window while another check of the same vendor and amount lands inside it.
const AmountHorizonDays = 30
