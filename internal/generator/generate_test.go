package generator

import (
	"testing"

	"reconpilot/internal/domain"
)

func TestGenerateIsDeterministicAndComplete(t *testing.T) {
	txs1, gt1 := Generate(42, 2000)
	txs2, gt2 := Generate(42, 2000)
	if len(txs1) != len(txs2) {
		t.Fatalf("same seed must give same size: %d vs %d", len(txs1), len(txs2))
	}
	for i := range txs1 {
		if txs1[i].DedupKey != txs2[i].DedupKey {
			t.Fatalf("row %d differs across runs with same seed", i)
		}
	}
	for _, typ := range []string{"commission", "refund", "partial", "timing", "duplicate", "missing", "unknown"} {
		if gt1.InjectedByType[typ] == 0 {
			t.Fatalf("type %s never injected: %+v", typ, gt1)
		}
	}
	if gt1.InjectedByType["refund"] != gt2.InjectedByType["refund"] {
		t.Fatal("ground truth must be deterministic too")
	}
	if len(gt1.IntendedGroup) == 0 || len(gt1.IntendedGroup) != len(gt2.IntendedGroup) {
		t.Fatalf("intended-group ground truth must be present and deterministic: %d vs %d",
			len(gt1.IntendedGroup), len(gt2.IntendedGroup))
	}
	ids := map[int64]bool{}
	for _, tx := range txs1 {
		if tx.ID == 0 || ids[tx.ID] {
			t.Fatalf("IDs must be unique and non-zero: %+v", tx)
		}
		ids[tx.ID] = true
		if tx.Source == domain.SourceMarketplace && tx.GrossKurus-tx.CommissionKurus != tx.AmountKurus {
			t.Fatalf("marketplace breakdown broken: %+v", tx)
		}
	}
	// Every IntendedGroup key must reference a generated transaction.
	for id := range gt1.IntendedGroup {
		if !ids[id] {
			t.Fatalf("intended-group id %d not in generated set", id)
		}
	}
}
