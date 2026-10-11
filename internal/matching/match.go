// Package matching runs the exact, tolerant and group passes in that order.
// SplitPool sorts each side by value date, reference and ID; the passes expect
// a pool built by SplitPool, which keeps results independent of input order.
package matching

import (
	"sort"
	"time"

	"reconpilot/internal/domain"
)

type Match struct {
	Kind    string // exact|tolerant|group
	ScoreBP int64  // basis points, 10000 = certain
	TxIDs   []int64
}

type Pool struct {
	Expected []domain.Transaction // PSP ledger: money we expect
	Actual   []domain.Transaction // bank + marketplace: money received
}

func SplitPool(txs []domain.Transaction) Pool {
	var p Pool
	for _, t := range txs {
		if t.Source.IsExpected() {
			p.Expected = append(p.Expected, t)
		} else {
			p.Actual = append(p.Actual, t)
		}
	}
	sortTxs(p.Expected)
	sortTxs(p.Actual)
	return p
}

func sortTxs(ts []domain.Transaction) {
	sort.Slice(ts, func(i, j int) bool {
		a, b := ts[i], ts[j]
		if !a.OccurredAt.Equal(b.OccurredAt) {
			return a.OccurredAt.Before(b.OccurredAt)
		}
		if a.ExternalRef != b.ExternalRef {
			return a.ExternalRef < b.ExternalRef
		}
		return a.ID < b.ID
	})
}

// dayDiff returns the absolute whole-day distance between two instants.
func dayDiff(a, b time.Time) int {
	d := int(a.Sub(b).Hours() / 24)
	if d < 0 {
		return -d
	}
	return d
}

// remove drops the elements at the given indices (must be sorted ascending).
func remove(ts []domain.Transaction, idx []int) []domain.Transaction {
	out := ts[:0:0]
	j := 0
	for i, t := range ts {
		if j < len(idx) && i == idx[j] {
			j++
			continue
		}
		out = append(out, t)
	}
	return out
}
