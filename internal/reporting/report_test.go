package reporting

import (
	"strings"
	"testing"
	"time"

	"reconpilot/internal/classification"
	"reconpilot/internal/domain"
	"reconpilot/internal/engine"
	"reconpilot/internal/matching"
)

func TestRenderContainsTotalsAndCSVRows(t *testing.T) {
	id := int64(3)
	all := []domain.Transaction{{ID: 3, Source: domain.SourcePSP, ExternalRef: "ORD-9",
		CounterpartyRef: "s", AmountKurus: 12345, Currency: "TRY",
		OccurredAt: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), Direction: domain.DirCredit}}
	out := engine.Output{
		Matches:       []matching.Match{{Kind: "exact", ScoreBP: 10000, TxIDs: []int64{1, 2}}},
		Discrepancies: []classification.Discrepancy{{TxID: &id, Type: "missing", DeltaKurus: 12345}},
		TotalTx:       3,
	}
	html, csvOut, err := Render(out, all)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"missing", "123,45", "exact"} {
		if !strings.Contains(string(html), want) {
			t.Fatalf("html must contain %q", want)
		}
	}
	if !strings.Contains(string(csvOut), "missing,3,12345") {
		t.Fatalf("csv row wrong:\n%s", csvOut)
	}
}
