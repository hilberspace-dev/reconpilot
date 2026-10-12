package matching

import (
	"reflect"
	"sort"
	"testing"

	"reconpilot/internal/domain"
)

func TestMatchSplits(t *testing.T) {
	p := SplitPool([]domain.Transaction{
		tx(1, domain.SourcePSP, "TX-5", 10000, 1),
		tx(2, domain.SourceBank, "TX-5", 6000, 2), // card
		tx(3, domain.SourceBank, "TX-5", 4000, 2), // voucher
	})
	matches := MatchSplits(&p)
	if len(matches) == 1 {
		sort.Slice(matches[0].TxIDs, func(i, j int) bool { return matches[0].TxIDs[i] < matches[0].TxIDs[j] })
	}
	if len(matches) != 1 || matches[0].Kind != "split" || !reflect.DeepEqual(matches[0].TxIDs, []int64{1, 2, 3}) {
		t.Fatalf("want one split match 1+2+3, got %+v", matches)
	}
	if len(p.Expected) != 0 || len(p.Actual) != 0 {
		t.Fatalf("split rows must leave the pool: %d %d", len(p.Expected), len(p.Actual))
	}

	for name, book := range map[string][]domain.Transaction{
		"parts short of the amount": {
			tx(1, domain.SourcePSP, "TX-5", 10000, 1),
			tx(2, domain.SourceBank, "TX-5", 6000, 2),
			tx(3, domain.SourceBank, "TX-5", 3000, 2),
		},
		"a part outside the window": {
			tx(1, domain.SourcePSP, "TX-5", 10000, 1),
			tx(2, domain.SourceBank, "TX-5", 6000, 2),
			tx(3, domain.SourceBank, "TX-5", 4000, 7),
		},
		"two open orders share the reference": {
			tx(1, domain.SourcePSP, "TX-5", 10000, 1),
			tx(4, domain.SourcePSP, "TX-5", 10000, 1),
			tx(2, domain.SourceBank, "TX-5", 6000, 2),
			tx(3, domain.SourceBank, "TX-5", 4000, 2),
		},
	} {
		p := SplitPool(book)
		if got := MatchSplits(&p); len(got) != 0 {
			t.Errorf("%s: want no split, got %+v", name, got)
		}
	}
}
