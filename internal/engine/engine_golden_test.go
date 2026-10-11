package engine_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reconpilot/internal/domain"
	"reconpilot/internal/engine"
	"reconpilot/internal/ingestion"
)

type goldenSummary struct {
	TotalTx       int            `json:"total_tx"`
	Matches       map[string]int `json:"matches"`
	Discrepancies map[string]int `json:"discrepancies"`
}

func TestGoldenDataset(t *testing.T) {
	root := filepath.Join("..", "..", "testdata", "golden")
	var all []domain.Transaction
	id := int64(0)
	for _, spec := range []struct {
		file string
		src  domain.SourceType
	}{{"psp.csv", domain.SourcePSP}, {"bank.csv", domain.SourceBank}, {"marketplace.csv", domain.SourceMarketplace}} {
		raw, err := os.ReadFile(filepath.Join(root, spec.file))
		if err != nil {
			t.Fatal(err)
		}
		a, _ := ingestion.AdapterFor(spec.src)
		txs, rejected, err := a.Parse(strings.NewReader(string(raw)))
		if err != nil || len(rejected) != 0 {
			t.Fatalf("%s: err=%v rejected=%d", spec.file, err, len(rejected))
		}
		for i := range txs {
			id++
			txs[i].ID = id
		}
		all = append(all, txs...)
	}
	out, err := engine.Run(all)
	if err != nil {
		t.Fatalf("engine: %v", err)
	}
	var want goldenSummary
	raw, _ := os.ReadFile(filepath.Join(root, "expected_summary.json"))
	if err := json.Unmarshal(raw, &want); err != nil {
		t.Fatal(err)
	}
	if out.TotalTx != want.TotalTx {
		t.Fatalf("total: want %d got %d", want.TotalTx, out.TotalTx)
	}
	gotM := map[string]int{}
	for _, m := range out.Matches {
		gotM[m.Kind]++
	}
	for kind, n := range want.Matches {
		if gotM[kind] != n {
			t.Fatalf("matches[%s]: want %d got %d (all: %v)", kind, n, gotM[kind], gotM)
		}
	}
	gotD := map[string]int{}
	for _, d := range out.Discrepancies {
		gotD[d.Type]++
	}
	for typ, n := range want.Discrepancies {
		if gotD[typ] != n {
			t.Fatalf("discrepancies[%s]: want %d got %d (all: %v)", typ, n, gotD[typ], gotD)
		}
	}
}
