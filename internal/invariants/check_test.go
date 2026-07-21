package invariants

import (
	"testing"
	"time"

	"reconpilot/internal/classification"
	"reconpilot/internal/domain"
	"reconpilot/internal/matching"
)

func tx(id int64, amount int64) domain.Transaction {
	return domain.Transaction{ID: id, Source: domain.SourcePSP, ExternalRef: "X",
		CounterpartyRef: "s", AmountKurus: amount, Currency: "TRY",
		OccurredAt: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), Direction: domain.DirCredit}
}

func idp(v int64) *int64 { return &v }

func TestCheckPassesWhenEveryTxIsPlaced(t *testing.T) {
	all := []domain.Transaction{tx(1, 100), tx(2, 100), tx(3, 50)}
	matches := []matching.Match{{Kind: "exact", ScoreBP: 10000, TxIDs: []int64{1, 2}}}
	discs := []classification.Discrepancy{{TxID: idp(3), Type: "missing", DeltaKurus: 50}}
	if err := Check(all, matches, discs); err != nil {
		t.Fatalf("want pass, got %v", err)
	}
}

func TestCheckFailsOnLostKurus(t *testing.T) {
	all := []domain.Transaction{tx(1, 100)} // neither matched nor classified
	if err := Check(all, nil, nil); err == nil {
		t.Fatal("invariant 1 violation must fail")
	}
}

func TestCheckFailsOnDoubleMembership(t *testing.T) {
	all := []domain.Transaction{tx(1, 100), tx(2, 100), tx(3, 100)}
	matches := []matching.Match{
		{Kind: "exact", TxIDs: []int64{1, 2}},
		{Kind: "exact", TxIDs: []int64{1, 3}},
	}
	if err := Check(all, matches, nil); err == nil {
		t.Fatal("invariant 2 violation must fail")
	}
}

func TestCheckFailsWhenDeltasDisagree(t *testing.T) {
	all := []domain.Transaction{tx(1, 100)}
	discs := []classification.Discrepancy{{TxID: idp(1), Type: "missing", DeltaKurus: 60}} // 60 != 100
	if err := Check(all, nil, discs); err == nil {
		t.Fatal("invariant 3 violation must fail")
	}
}
