# ADR 0004 — Invariants enforced at the schema level, not only in code

**Status:** accepted · 2026-07-22

## Context

Application-level checks can be refactored away, skipped by a new code
path, or silently weakened. Two of the four correctness guarantees — "no
transaction belongs to more than one match group" and "re-ingesting the
same file is a no-op" — admit a stronger guarantee: the database itself can
refuse the violating write.

## Decision

- `match_member.transaction_id` carries `UNIQUE` — invariant 2 becomes a
  hard constraint no code path can bypass.
- `transaction.dedup_key` carries `UNIQUE` with `ON CONFLICT DO NOTHING`
  on insert — invariant 4 (idempotent ingest) at the schema level.
- `discrepancy` carries `CHECK (transaction_id IS NOT NULL OR
  match_group_id IS NOT NULL)` — an ownerless discrepancy row, which would
  silently break the result's ownership model, cannot exist.

The application-level checker (`internal/invariants`) validates guarantees
1–3 on every engine run, including type-aware delta semantics. Guarantee 4
belongs to ingestion: PostgreSQL enforces it through `UNIQUE(dedup_key)`, and
an integration test verifies the behaviour against real PostgreSQL via
testcontainers. The schema is also defence in depth for guarantee 2.

## Consequences

- A double-membership bug anywhere in matching or persistence surfaces as
  an immediate database error, never as a quietly wrong report.
- The covered double-membership, duplicate-ingestion and ownerless-row
  failures surface explicitly instead of passing silently.
