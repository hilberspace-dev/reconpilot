package domain

import (
	"testing"
	"time"
)

func TestDedupKeyDeterministicAndDiscriminating(t *testing.T) {
	at := time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC)
	a := DedupKey(SourcePSP, "TX-1", 10050, at, DirCredit)
	b := DedupKey(SourcePSP, "TX-1", 10050, at, DirCredit)
	if a != b {
		t.Fatalf("same input must give same key: %s vs %s", a, b)
	}
	if len(a) != 64 {
		t.Fatalf("want 64-char hex sha256, got len %d", len(a))
	}
	if DedupKey(SourcePSP, "TX-1", 10051, at, DirCredit) == a {
		t.Fatal("different amount must change key")
	}
	if DedupKey(SourceBank, "TX-1", 10050, at, DirCredit) == a {
		t.Fatal("different source must change key")
	}
	// Same instant in another zone must produce the SAME key (UTC normalisation).
	ist := time.FixedZone("TRT", 3*3600)
	if DedupKey(SourcePSP, "TX-1", 10050, at.In(ist), DirCredit) != a {
		t.Fatal("zone change of same instant must not change key")
	}
}
