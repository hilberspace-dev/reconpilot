package engine_test

import (
	"fmt"
	"reflect"
	"sort"
	"testing"
	"time"

	"pgregory.net/rapid"
	"reconpilot/internal/domain"
	"reconpilot/internal/engine"
)

// For any mix of paired, shifted, missing and noise transactions, engine.Run
// must place every transaction without error. The oracle is invariants.Check
// inside Run; rapid shrinks a failure to a minimal case.
func TestEngineInvariantsHoldForArbitraryInput(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		var all []domain.Transaction
		id := int64(0)
		nextID := func() int64 { id++; return id }
		day0 := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)

		nPairs := rapid.IntRange(0, 30).Draw(rt, "pairs")
		for i := 0; i < nPairs; i++ {
			amount := rapid.Int64Range(1, 5_000_00).Draw(rt, fmt.Sprintf("amt%d", i))
			day := rapid.IntRange(0, 20).Draw(rt, fmt.Sprintf("day%d", i))
			shift := rapid.IntRange(0, 6).Draw(rt, fmt.Sprintf("shift%d", i))
			ref := fmt.Sprintf("ORD-%d", i)
			all = append(all, domain.Transaction{ID: nextID(), Source: domain.SourcePSP,
				ExternalRef: ref, CounterpartyRef: "s1", AmountKurus: amount, Currency: "TRY",
				OccurredAt: day0.AddDate(0, 0, day), Direction: domain.DirCredit})
			all = append(all, domain.Transaction{ID: nextID(), Source: domain.SourceBank,
				ExternalRef: ref, CounterpartyRef: "s1", AmountKurus: amount, Currency: "TRY",
				OccurredAt: day0.AddDate(0, 0, day+shift), Direction: domain.DirCredit})
		}
		nLoners := rapid.IntRange(0, 15).Draw(rt, "loners")
		for i := 0; i < nLoners; i++ {
			amount := rapid.Int64Range(1, 5_000_00).Draw(rt, fmt.Sprintf("lamt%d", i))
			src := domain.SourcePSP
			if rapid.Bool().Draw(rt, fmt.Sprintf("side%d", i)) {
				src = domain.SourceBank
			}
			all = append(all, domain.Transaction{ID: nextID(), Source: src,
				ExternalRef: fmt.Sprintf("LON-%d", i), CounterpartyRef: "s1",
				AmountKurus: amount, Currency: "TRY",
				OccurredAt: day0, Direction: domain.DirCredit})
		}
		if _, err := engine.Run(all); err != nil {
			rt.Fatalf("invariant violated on generated input: %v", err)
		}
	})
}

// Books drawn from two sellers, six weeks and three amounts collide all the
// time, and bank references are empty, agreeing, foreign or naming another
// order. Whatever the draw, a same-amount pair matched without an agreeing
// reference must have had no rival: no other row of that seller and amount,
// left open at the end, within the horizon of its partner and free of a
// reference conflict. No match may pair rows whose references name different
// records of one counterparty.
func TestEngineNeverChoosesBetweenEqualCandidates(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		day0 := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
		amounts := []int64{1000, 2000, 2500}
		var all []domain.Transaction
		id := int64(0)
		draw := func(src domain.SourceType, i int, ref string) domain.Transaction {
			id++
			tx := domain.Transaction{ID: id, Source: src, ExternalRef: ref,
				CounterpartyRef: rapid.SampledFrom([]string{"s1", "s2"}).Draw(rt, fmt.Sprintf("cp%d", id)),
				AmountKurus:     rapid.SampledFrom(amounts).Draw(rt, fmt.Sprintf("amt%d", id)),
				Currency:        "TRY",
				OccurredAt:      day0.AddDate(0, 0, rapid.IntRange(0, 40).Draw(rt, fmt.Sprintf("day%d", id))),
				Direction:       domain.DirCredit}
			if rapid.IntRange(0, 9).Draw(rt, fmt.Sprintf("dir%d", id)) == 0 {
				tx.Direction = domain.DirDebit
			}
			return tx
		}
		nExp := rapid.IntRange(0, 12).Draw(rt, "expected")
		for i := 0; i < nExp; i++ {
			ref := fmt.Sprintf("ORD-%d", i)
			if rapid.IntRange(0, 4).Draw(rt, fmt.Sprintf("noref%d", i)) == 0 {
				ref = ""
			}
			all = append(all, draw(domain.SourcePSP, i, ref))
		}
		nAct := rapid.IntRange(0, 12).Draw(rt, "actual")
		for i := 0; i < nAct; i++ {
			ref := ""
			switch rapid.IntRange(0, 3).Draw(rt, fmt.Sprintf("refkind%d", i)) {
			case 1:
				ref = fmt.Sprintf("ORD-%d", rapid.IntRange(0, 12).Draw(rt, fmt.Sprintf("names%d", i)))
			case 2:
				ref = fmt.Sprintf("STMT-%d", i)
			}
			all = append(all, draw(domain.SourceBank, i, ref))
		}
		out, err := engine.Run(all)
		if err != nil {
			rt.Fatalf("invariant violated: %v", err)
		}

		// A reference names a record within its counterparty.
		type record struct{ cp, ref string }
		expRefs, actRefs := map[record]bool{}, map[record]bool{}
		for _, t := range all {
			if t.ExternalRef == "" {
				continue
			}
			if t.Source.IsExpected() {
				expRefs[record{t.CounterpartyRef, t.ExternalRef}] = true
			} else {
				actRefs[record{t.CounterpartyRef, t.ExternalRef}] = true
			}
		}
		agree := func(a, b domain.Transaction) bool {
			return a.ExternalRef != "" && a.ExternalRef == b.ExternalRef && a.CounterpartyRef == b.CounterpartyRef
		}
		conflict := func(e, a domain.Transaction) bool {
			return !agree(e, a) && (actRefs[record{e.CounterpartyRef, e.ExternalRef}] || expRefs[record{a.CounterpartyRef, a.ExternalRef}])
		}
		byID := map[int64]domain.Transaction{}
		for _, t := range all {
			byID[t.ID] = t
		}
		matched := map[int64]bool{}
		for _, m := range out.Matches {
			for _, id := range m.TxIDs {
				matched[id] = true
			}
		}
		// rival reports whether x fits y's partner as well as y does: same
		// seller, amount and direction, and within the horizon of the partner.
		rival := func(x, partner domain.Transaction) bool {
			d := x.OccurredAt.Sub(partner.OccurredAt)
			if d < 0 {
				d = -d
			}
			return x.CounterpartyRef == partner.CounterpartyRef && x.AmountKurus == partner.AmountKurus &&
				x.Direction == partner.Direction && int(d.Hours()/24) <= domain.AmountHorizonDays
		}
		for _, m := range out.Matches {
			var e, a domain.Transaction
			for _, id := range m.TxIDs {
				if t := byID[id]; t.Source.IsExpected() {
					e = t
				} else {
					a = t
				}
				for _, other := range m.TxIDs {
					if x, y := byID[id], byID[other]; x.Source.IsExpected() && !y.Source.IsExpected() && conflict(x, y) {
						rt.Fatalf("match %v pairs %d and %d, whose references name different records", m.TxIDs, x.ID, y.ID)
					}
				}
			}
			if m.Kind != "tolerant" || agree(e, a) || e.AmountKurus != a.AmountKurus {
				continue
			}
			for _, x := range all {
				if matched[x.ID] || x.ID == e.ID || x.ID == a.ID {
					continue
				}
				if x.Source.IsExpected() && rival(x, a) && !conflict(x, a) {
					rt.Fatalf("matched %d-%d while %d fitted %d just as well", e.ID, a.ID, x.ID, a.ID)
				}
				if x.Source == domain.SourceBank && rival(x, e) && !conflict(e, x) {
					rt.Fatalf("matched %d-%d while %d fitted %d just as well", e.ID, a.ID, x.ID, e.ID)
				}
			}
		}
	})
}

// The store numbers rows in the order a file lists them, so the same book can
// arrive with its rows in another order and other IDs. The outcome must be the
// same row content in the same matches and with the same types.
func TestEngineResultDependsOnContentNotLoadOrder(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		day0 := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
		var book []domain.Transaction
		n := rapid.IntRange(0, 24).Draw(rt, "rows")
		for i := 0; i < n; i++ {
			src := rapid.SampledFrom([]domain.SourceType{domain.SourcePSP, domain.SourceBank}).Draw(rt, fmt.Sprintf("src%d", i))
			tx := domain.Transaction{Source: src, Currency: "TRY",
				ExternalRef:     rapid.SampledFrom([]string{"", "R1", "R2", "R3"}).Draw(rt, fmt.Sprintf("ref%d", i)),
				CounterpartyRef: rapid.SampledFrom([]string{"s1", "s2"}).Draw(rt, fmt.Sprintf("cp%d", i)),
				AmountKurus:     rapid.SampledFrom([]int64{1000, 2000, 2995, 3000}).Draw(rt, fmt.Sprintf("amt%d", i)),
				OccurredAt:      day0.AddDate(0, 0, rapid.IntRange(0, 8).Draw(rt, fmt.Sprintf("day%d", i))),
				Direction:       domain.DirCredit}
			if rapid.IntRange(0, 5).Draw(rt, fmt.Sprintf("dir%d", i)) == 0 {
				tx.Direction = domain.DirDebit
			}
			book = append(book, tx)
		}
		perm := rapid.Permutation(book).Draw(rt, "order")
		number := func(rows []domain.Transaction) []domain.Transaction {
			out := append([]domain.Transaction(nil), rows...)
			for i := range out {
				out[i].ID = int64(i + 1)
			}
			return out
		}
		canon := func(rows []domain.Transaction) []string {
			out, err := engine.Run(rows)
			if err != nil {
				rt.Fatalf("invariant violated: %v", err)
			}
			byID := map[int64]domain.Transaction{}
			for _, t := range rows {
				byID[t.ID] = t
			}
			key := func(t domain.Transaction) string {
				return fmt.Sprintf("%s/%s/%s/%d/%s/%s", t.Source, t.ExternalRef, t.CounterpartyRef, t.AmountKurus,
					t.OccurredAt.Format("2006-01-02"), t.Direction)
			}
			var got []string
			for _, m := range out.Matches {
				var members []string
				for _, id := range m.TxIDs {
					members = append(members, key(byID[id]))
				}
				sort.Strings(members)
				got = append(got, fmt.Sprintf("match %s %v", m.Kind, members))
			}
			for _, d := range out.Discrepancies {
				if d.TxID != nil {
					got = append(got, fmt.Sprintf("%s %d %s", d.Type, d.DeltaKurus, key(byID[*d.TxID])))
				}
			}
			sort.Strings(got)
			return got
		}
		if a, b := canon(number(book)), canon(number(perm)); !reflect.DeepEqual(a, b) {
			rt.Fatalf("outcome changed with load order:\n%v\n%v", a, b)
		}
	})
}
