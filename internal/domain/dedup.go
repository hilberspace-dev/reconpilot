package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"
)

// DedupKey is the idempotency key for ingest (invariant 4). The timestamp is
// normalised to UTC so the same instant always produces the same key.
func DedupKey(src SourceType, ref string, amountKurus int64, occurredAt time.Time, dir Direction) string {
	h := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%d|%s|%s",
		src, ref, amountKurus, occurredAt.UTC().Format(time.RFC3339), dir)))
	return hex.EncodeToString(h[:])
}

// OccurrenceKey keys the n-th row of one file that has the same DedupKey as an
// earlier row (n counts from 2). A statement can list two genuine lines that
// agree on every keyed field, such as two equal payments on one day with no
// reference. Reading the same file again numbers them the same way, so a
// re-ingest still adds nothing.
func OccurrenceKey(key string, n int) string {
	h := sha256.Sum256([]byte(fmt.Sprintf("%s#%d", key, n)))
	return hex.EncodeToString(h[:])
}
