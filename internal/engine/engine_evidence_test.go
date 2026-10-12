package engine_test

import (
	"fmt"
	"reflect"
	"sort"
	"testing"
	"time"

	"reconpilot/internal/domain"
	"reconpilot/internal/engine"
)

// Each case is a shape found when the engine was run on public real data
// (a bank-vs-ledger set and a marketplace orders-vs-payments set), reduced to
// a few rows. The want map gives every row's outcome: the match kind it ended
// up in, or its discrepancy type.
func TestEngineEvidenceCases(t *testing.T) {
	day := func(d int) time.Time { return time.Date(2026, 7, d, 0, 0, 0, 0, time.UTC) }
	row := func(id int64, src domain.SourceType, ref, cp string, amount int64, d int, dir domain.Direction) domain.Transaction {
		return domain.Transaction{ID: id, Source: src, ExternalRef: ref, CounterpartyRef: cp,
			AmountKurus: amount, Currency: "TRY", OccurredAt: day(d), Direction: dir}
	}
	const (
		psp  = domain.SourcePSP
		bank = domain.SourceBank
		cr   = domain.DirCredit
		dr   = domain.DirDebit
	)
	cases := []struct {
		name string
		book []domain.Transaction
		want map[int64]string
	}{
		{
			name: "unreferenced lines of one amount are not duplicates",
			book: []domain.Transaction{
				row(1, bank, "", "s1", 4000, 3, cr),
				row(2, bank, "", "s2", 4000, 3, cr),
			},
			want: map[int64]string{1: "unknown", 2: "unknown"},
		},
		{
			name: "two equal orders and two equal unreferenced lines stay open",
			book: []domain.Transaction{
				row(1, psp, "TX-1", "s1", 40000, 5, cr),
				row(2, psp, "TX-2", "s1", 40000, 5, cr),
				row(3, bank, "", "s1", 40000, 5, cr),
				row(4, bank, "", "s1", 40000, 5, cr),
			},
			want: map[int64]string{1: "ambiguous", 2: "ambiguous", 3: "ambiguous", 4: "ambiguous"},
		},
		{
			name: "a line naming another order is not this order's money",
			book: []domain.Transaction{
				row(1, psp, "TX-1", "s1", 100000, 1, cr),
				row(2, psp, "TX-2", "s1", 180000, 1, cr),
				row(3, bank, "TX-2", "s1", 100000, 2, cr),
			},
			want: map[int64]string{1: "missing", 2: "partial", 3: "partial"},
		},
		{
			name: "an order paid by voucher and card is one split match",
			book: []domain.Transaction{
				row(1, psp, "TX-5", "s1", 10000, 1, cr),
				row(2, bank, "TX-5", "s1", 6000, 1, cr),
				row(3, bank, "TX-5", "s1", 4000, 1, cr),
			},
			want: map[int64]string{1: "split", 2: "split", 3: "split"},
		},
		{
			name: "a split paid in full but late is timing",
			book: []domain.Transaction{
				row(1, psp, "TX-5", "s1", 10000, 1, cr),
				row(2, bank, "TX-5", "s1", 6000, 6, cr),
				row(3, bank, "TX-5", "s1", 4000, 6, cr),
			},
			want: map[int64]string{1: "timing", 2: "timing", 3: "timing"},
		},
		{
			name: "a sale and its void under one reference reverse each other",
			book: []domain.Transaction{
				row(1, psp, "TX-9", "s1", 5000, 2, cr),
				row(2, psp, "TX-9", "s1", 5000, 2, dr),
			},
			want: map[int64]string{1: "reversal", 2: "reversal"},
		},
		{
			name: "a check number reused for another vendor is another record",
			book: []domain.Transaction{
				row(1, psp, "CHK-7", "v1", 50000, 1, cr),
				row(2, psp, "CHK-7", "v2", 20000, 1, cr),
				row(3, bank, "CHK-7", "v1", 50000, 9, cr),
				row(4, bank, "CHK-7", "v2", 20000, 9, cr),
			},
			want: map[int64]string{1: "timing", 2: "timing", 3: "timing", 4: "timing"},
		},
		{
			name: "an unreferenced bank debit is not a refund",
			book: []domain.Transaction{
				row(1, bank, "", "s1", 3000, 2, dr),
				row(2, bank, "", "s1", 7000, 1, cr),
			},
			want: map[int64]string{1: "unknown", 2: "unknown"},
		},
		{
			name: "two equal parts of a late split are both timing",
			book: []domain.Transaction{
				row(1, psp, "TX-5", "s1", 10000, 1, cr),
				row(2, bank, "TX-5", "s1", 5000, 10, cr),
				row(3, bank, "TX-5", "s1", 5000, 10, cr),
			},
			want: map[int64]string{1: "timing", 2: "timing", 3: "timing"},
		},
		{
			name: "the same amount 40 days later is not this order's money without a reference",
			book: []domain.Transaction{
				row(1, psp, "X1", "s1", 5000, 1, cr),
				row(2, bank, "", "s1", 5000, 41, cr),
			},
			want: map[int64]string{1: "missing", 2: "unknown"},
		},
		{
			name: "an overpaid order is not timing for another order of the seller",
			book: []domain.Transaction{
				row(1, psp, "TX-1", "s1", 100000, 12, cr),
				row(2, bank, "TX-1", "s1", 103000, 12, cr),
				row(3, psp, "TX-7", "s1", 103000, 1, cr),
			},
			want: map[int64]string{1: "unknown", 2: "unknown", 3: "missing"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, err := engine.Run(c.book)
			if err != nil {
				t.Fatal(err)
			}
			got := map[int64]string{}
			for _, m := range out.Matches {
				for _, id := range m.TxIDs {
					got[id] = m.Kind
				}
			}
			for _, d := range out.Discrepancies {
				if d.TxID != nil {
					got[*d.TxID] = d.Type
				}
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("got %s, want %s", outcomes(got), outcomes(c.want))
			}
		})
	}
}

func outcomes(m map[int64]string) string {
	ids := make([]int64, 0, len(m))
	for id := range m {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	s := ""
	for _, id := range ids {
		s += fmt.Sprintf(" %d:%s", id, m[id])
	}
	return s
}

// The group pass runs after the tolerant pass, and a payout whose gross two
// sets of orders add up to leaves those orders to a person.
func TestEngineGroupPassAndAmbiguity(t *testing.T) {
	day := func(d int) time.Time { return time.Date(2026, 7, d, 0, 0, 0, 0, time.UTC) }
	order := func(id int64, ref string, amount int64, d int) domain.Transaction {
		return domain.Transaction{ID: id, Source: domain.SourcePSP, ExternalRef: ref, CounterpartyRef: "s1",
			AmountKurus: amount, Currency: "TRY", OccurredAt: day(d), Direction: domain.DirCredit}
	}
	payout := func(id int64, gross, commission int64, d int) domain.Transaction {
		return domain.Transaction{ID: id, Source: domain.SourceMarketplace, ExternalRef: fmt.Sprintf("PAY-%d", id),
			CounterpartyRef: "s1", AmountKurus: gross - commission, GrossKurus: gross, CommissionKurus: commission,
			Currency: "TRY", OccurredAt: day(d), Direction: domain.DirCredit}
	}
	outcome := func(t *testing.T, book []domain.Transaction) map[int64]string {
		t.Helper()
		out, err := engine.Run(book)
		if err != nil {
			t.Fatal(err)
		}
		got := map[int64]string{}
		for _, m := range out.Matches {
			for _, id := range m.TxIDs {
				got[id] = m.Kind
			}
		}
		for _, d := range out.Discrepancies {
			if d.TxID != nil {
				got[*d.TxID] = d.Type
			}
		}
		return got
	}

	t.Run("a later payout cannot settle an ambiguous one by elimination", func(t *testing.T) {
		// Payout 8 is 1000+2000 or 3000. Payout 9 (1000) would take order 1
		// and so decide payout 8 by elimination.
		got := outcome(t, []domain.Transaction{
			order(1, "ORD-1", 1000, 1), order(2, "ORD-2", 2000, 1), order(3, "ORD-3", 3000, 1),
			payout(8, 3000, 0, 5), payout(9, 1000, 0, 6),
		})
		for _, id := range []int64{1, 2, 3, 8} {
			if got[id] != "ambiguous" {
				t.Fatalf("row %d is %q, want ambiguous (all: %s)", id, got[id], outcomes(got))
			}
		}
		if got[9] == "group" {
			t.Fatalf("payout 9 took a contested order: %s", outcomes(got))
		}
	})

	t.Run("a later payout that needs a contested order is not matched without it", func(t *testing.T) {
		// Payout 9 may be order 3 plus order 4. Searching it without the
		// contested orders would leave order 5 as its only fit and match it.
		got := outcome(t, []domain.Transaction{
			order(1, "ORD-1", 1000, 1), order(2, "ORD-2", 2000, 1), order(3, "ORD-3", 3000, 1),
			order(4, "ORD-4", 1000, 6), order(5, "ORD-5", 4000, 6),
			payout(8, 3000, 0, 5), payout(9, 4000, 0, 6),
		})
		if got[9] == "group" || got[5] == "group" {
			t.Fatalf("payout 9 was matched although its own set may use a contested order: %s", outcomes(got))
		}
	})

	t.Run("a payout refused over a contested order passes the contest on", func(t *testing.T) {
		// Payout 8 is 1000+2000 or 3000. Payout 9 (3700) can only be order 3
		// plus order 4, and order 3 is contested, so 9 is refused and order 4
		// becomes contested too. Payout 10 (700) would otherwise take order 4.
		got := outcome(t, []domain.Transaction{
			order(1, "ORD-1", 1000, 1), order(2, "ORD-2", 2000, 1), order(3, "ORD-3", 3000, 3),
			order(4, "ORD-4", 700, 10),
			payout(8, 3000, 0, 5), payout(9, 3700, 0, 16), payout(10, 700, 0, 17),
		})
		for _, id := range []int64{8, 9, 10} {
			if got[id] == "group" {
				t.Fatalf("payout %d was matched: %s", id, outcomes(got))
			}
		}
		if got[4] != "ambiguous" {
			t.Fatalf("order 4 is %q, want ambiguous: %s", got[4], outcomes(got))
		}
	})

	t.Run("a rival the group pass took no longer blocks a bank pair", func(t *testing.T) {
		// Order 2 could have been line 3's source until the payout took it.
		got := outcome(t, []domain.Transaction{
			order(1, "ORD-1", 1000, 1), order(2, "ORD-2", 1000, 18),
			{ID: 3, Source: domain.SourceBank, CounterpartyRef: "s1", AmountKurus: 1000, Currency: "TRY",
				OccurredAt: day(3), Direction: domain.DirCredit},
			payout(4, 1000, 0, 20),
		})
		want := map[int64]string{1: "tolerant", 2: "group", 3: "tolerant", 4: "group"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("got %s, want %s", outcomes(got), outcomes(want))
		}
	})

	t.Run("a bank line whose only candidate the group pass took is not ambiguous", func(t *testing.T) {
		got := outcome(t, []domain.Transaction{
			order(1, "GRP-1", 1000, 1),
			{ID: 2, Source: domain.SourceBank, CounterpartyRef: "s1", AmountKurus: 996, Currency: "TRY",
				OccurredAt: day(2), Direction: domain.DirCredit},
			payout(3, 1000, 100, 5),
		})
		want := map[int64]string{1: "group", 2: "unknown", 3: "group"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("got %s, want %s", outcomes(got), outcomes(want))
		}
	})
}

// An order paid short in two lines of one day: which line the partial rule
// measures against must not depend on which line was loaded first.
func TestEngineShortPaymentDoesNotDependOnLoadOrder(t *testing.T) {
	day := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	line := func(id, amount int64) domain.Transaction {
		return domain.Transaction{ID: id, Source: domain.SourceBank, ExternalRef: "TX-1", CounterpartyRef: "s1",
			AmountKurus: amount, Currency: "TRY", OccurredAt: day, Direction: domain.DirCredit}
	}
	order := domain.Transaction{ID: 1, Source: domain.SourcePSP, ExternalRef: "TX-1", CounterpartyRef: "s1",
		AmountKurus: 3000, Currency: "TRY", OccurredAt: day, Direction: domain.DirCredit}
	deltaOf := func(book []domain.Transaction) string {
		out, err := engine.Run(book)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range out.Discrepancies {
			if d.TxID != nil && *d.TxID == 1 {
				return fmt.Sprintf("%s %d", d.Type, d.DeltaKurus)
			}
		}
		return "none"
	}
	a := deltaOf([]domain.Transaction{order, line(2, 2000), line(3, 500)})
	b := deltaOf([]domain.Transaction{order, line(2, 500), line(3, 2000)})
	if a != b {
		t.Fatalf("the order's discrepancy changed with load order: %q vs %q", a, b)
	}
}
