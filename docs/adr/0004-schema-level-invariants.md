# ADR 0004: Invariants enforced at the schema level

**Status:** accepted · 2026-07-22

## Context

Application-level checks can be refactored away, skipped by a new code
path or weakened without anyone noticing. Two of the four correctness
guarantees ("no transaction belongs to more than one match group" and
"re-ingesting the same file adds no transactions") can be held more firmly than that,
because the database itself can refuse the write that would break them.

## Decision

- `match_member.transaction_id` is `UNIQUE`, so invariant 2 is a hard
  constraint that no code path can bypass.
- `transaction.dedup_key` is `UNIQUE` and inserts use
  `ON CONFLICT DO NOTHING`, which puts invariant 4 (re-ingesting adds no transactions) in
  the schema.
- `discrepancy` has a check constraint,
  `CHECK (transaction_id IS NOT NULL OR match_group_id IS NOT NULL)`, so a
  discrepancy row with no owner cannot exist. Such a row would silently
  break the result's ownership model.

The application-level checker (`internal/invariants`) validates guarantees
1–3 on every engine run, including type-aware delta semantics. Guarantee 4
belongs to ingestion. PostgreSQL enforces it through `UNIQUE(dedup_key)`,
and an integration test checks the behaviour against a real PostgreSQL
started by testcontainers. The schema is also defence in depth for
guarantee 2.

Ingest is atomic per file. `ingestion.Ingest` parses the whole file before
it touches the database; a file with a bad header stores nothing. It then
stores the batch, its transactions, its rejected rows and the batch counts
(`status = 'ingested'`, `row_count`, `rejected_count`) in one database
transaction, and if any statement fails nothing from the file is kept.

The integration tests carry the `integration` build tag. `go test ./...`
runs without Docker; `go test -tags integration ./...` needs Docker, and
that is the form CI runs.

## Consequences

- A double-membership bug anywhere in matching or persistence fails the
  write with a database error.
- The database deals with all three covered failures: it refuses a double
  membership or an ownerless row with an error, and it skips a duplicate
  transaction, which ingest then counts as deduped.
