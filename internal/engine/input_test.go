package engine_test

import (
	"fmt"
	"math"
	"reflect"
	"sort"
	"testing"
	"time"

	"reconpilot/internal/domain"
	"reconpilot/internal/engine"
)

func bookRow(id int64, src domain.SourceType, amount int64) domain.Transaction {
	return domain.Transaction{ID: id, Source: src, ExternalRef: "order", CounterpartyRef: "shop",
		AmountKurus: amount, Currency: "TRY", Direction: domain.DirCredit,
		OccurredAt: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)}
}

func TestRunRejectsUnsupportedMoney(t *testing.T) {
	good := []domain.Transaction{bookRow(1, domain.SourcePSP, 1000), bookRow(2, domain.SourceBank, 1000)}
	if out, err := engine.Run(good); err != nil || len(out.Matches) != 1 {
		t.Fatalf("valid control: %+v, %v", out, err)
	}
	for _, currency := range []string{"USD", "", "try", " TRY"} {
		bad := append([]domain.Transaction(nil), good...)
		bad[1].Currency = currency
		if _, err := engine.Run(bad); err == nil {
			t.Errorf("accepted currency %q", currency)
		}
	}
	bad := append([]domain.Transaction(nil), good...)
	bad[0].AmountKurus = -1
	if _, err := engine.Run(bad); err == nil {
		t.Error("accepted negative amount")
	}
}

func TestRunRejectsOverflowingBook(t *testing.T) {
	all := []domain.Transaction{bookRow(1, domain.SourcePSP, math.MaxInt64), bookRow(2, domain.SourceBank, 1)}
	if _, err := engine.Run(all); err == nil {
		t.Fatal("accepted a book whose total exceeds int64")
	}
}

func TestLargeTolerantMatch(t *testing.T) {
	amount := int64(math.MaxInt64/50 + 1)
	all := []domain.Transaction{bookRow(1, domain.SourcePSP, amount), bookRow(2, domain.SourceBank, amount)}
	all[1].ExternalRef = "bank-ref"
	out, err := engine.Run(all)
	if err != nil || len(out.Matches) != 1 || out.Matches[0].Kind != "tolerant" {
		t.Fatalf("large equal amounts: %+v, %v", out, err)
	}
}

func TestClassificationIgnoresInputOrder(t *testing.T) {
	all := []domain.Transaction{bookRow(1, domain.SourcePSP, 1000), bookRow(2, domain.SourceBank, 400), bookRow(3, domain.SourceBank, 850)}
	want, err := engine.Run(all)
	if err != nil {
		t.Fatal(err)
	}
	all[1], all[2] = all[2], all[1]
	got, err := engine.Run(all)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("permutation changed result: want %+v, got %+v, %v", want, got, err)
	}
}

func discrepancies(t *testing.T, all []domain.Transaction) []string {
	t.Helper()
	out, err := engine.Run(all)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, d := range out.Discrepancies {
		if d.TxID != nil {
			got = append(got, fmt.Sprintf("%d %s %d", *d.TxID, d.Type, d.DeltaKurus))
		}
	}
	sort.Strings(got)
	return got
}

// An outgoing refund is not part of an expected incoming payment: the credit
// stays missing in full and the debit is a refund.
func TestDebitIsNotACreditShortfall(t *testing.T) {
	all := []domain.Transaction{bookRow(1, domain.SourcePSP, 1000), bookRow(2, domain.SourceBank, 400)}
	all[1].Direction = domain.DirDebit
	want := []string{"1 missing 1000", "2 refund -400"}
	if got := discrepancies(t, all); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestLateDebitIsNotTiming(t *testing.T) {
	all := []domain.Transaction{bookRow(1, domain.SourcePSP, 1000), bookRow(2, domain.SourceBank, 1000)}
	all[1].Direction = domain.DirDebit
	all[1].OccurredAt = all[0].OccurredAt.AddDate(0, 0, 6)
	want := []string{"1 missing 1000", "2 refund -1000"}
	if got := discrepancies(t, all); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// An expected debit with a smaller actual credit under the same reference is
// neither a partial payment nor its mirror.
func TestExpectedDebitIgnoresActualCredit(t *testing.T) {
	all := []domain.Transaction{bookRow(1, domain.SourcePSP, 1000), bookRow(2, domain.SourceBank, 400)}
	all[0].Direction = domain.DirDebit
	want := []string{"1 missing 1000", "2 unknown 400"}
	if got := discrepancies(t, all); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestRunRejectsDuplicateIDs(t *testing.T) {
	all := []domain.Transaction{bookRow(1, domain.SourcePSP, 1000), bookRow(1, domain.SourceBank, 1000)}
	if _, err := engine.Run(all); err == nil {
		t.Fatal("accepted two transactions with the same ID")
	}
}

func TestRunRejectsCommissionAboveGross(t *testing.T) {
	payout := bookRow(2, domain.SourceMarketplace, 0)
	payout.GrossKurus, payout.CommissionKurus = 100, 150
	if _, err := engine.Run([]domain.Transaction{bookRow(1, domain.SourcePSP, 100), payout}); err == nil {
		t.Fatal("accepted a payout whose commission exceeds its gross")
	}
}
