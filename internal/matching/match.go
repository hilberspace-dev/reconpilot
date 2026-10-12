// Package matching runs the exact, split, tolerant and group passes in that
// order. SplitPool sorts each side by row content (value date, reference,
// counterparty, amount, direction, source, gross) and only then by ID; the
// passes expect a pool built by SplitPool. IDs follow the order rows were
// loaded in, so the result depends on that order only among rows that are
// identical in every field the engine reads.
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

	// References on each side of the whole book, matched rows included.
	expectedRefs, actualRefs map[refKey]bool
}

// refKey names one record: a reference is unique only within its counterparty.
type refKey struct{ cp, ref string }

func keyOf(t domain.Transaction) refKey { return refKey{t.CounterpartyRef, t.ExternalRef} }

func SplitPool(txs []domain.Transaction) Pool {
	p := Pool{expectedRefs: map[refKey]bool{}, actualRefs: map[refKey]bool{}}
	for _, t := range txs {
		if t.Source.IsExpected() {
			p.Expected = append(p.Expected, t)
			if t.ExternalRef != "" {
				p.expectedRefs[keyOf(t)] = true
			}
		} else {
			p.Actual = append(p.Actual, t)
			if t.ExternalRef != "" {
				p.actualRefs[keyOf(t)] = true
			}
		}
	}
	sortTxs(p.Expected)
	sortTxs(p.Actual)
	return p
}

// RefsAgree reports whether two rows carry the same reference for the same
// counterparty. An empty reference agrees with nothing.
func RefsAgree(a, b domain.Transaction) bool {
	return a.ExternalRef != "" && a.ExternalRef == b.ExternalRef && a.CounterpartyRef == b.CounterpartyRef
}

// RefConflict reports whether one of the two rows carries a reference that
// names a different record on the other side of the book. A reference nobody
// on the other side uses (a bank's own statement number) says nothing.
func (p *Pool) RefConflict(expected, actual domain.Transaction) bool {
	if RefsAgree(expected, actual) {
		return false
	}
	return p.actualRefs[keyOf(expected)] || p.expectedRefs[keyOf(actual)]
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
		if a.CounterpartyRef != b.CounterpartyRef {
			return a.CounterpartyRef < b.CounterpartyRef
		}
		if a.AmountKurus != b.AmountKurus {
			return a.AmountKurus < b.AmountKurus
		}
		if a.Direction != b.Direction {
			return a.Direction < b.Direction
		}
		if a.Source != b.Source {
			return a.Source < b.Source
		}
		if a.GrossKurus != b.GrossKurus {
			return a.GrossKurus < b.GrossKurus
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
