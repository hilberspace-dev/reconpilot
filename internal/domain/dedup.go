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
