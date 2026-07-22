# ADR 0004 — Invariants enforced at the schema level, not only in code

**Status:** accepted · 2026-07-22

## Context

Application-level checks can be refactored away, skipped by a new code
path, or silently weakened. Two of the four correctness invariants — "no
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
  silently corrupt invariant 1's accounting, cannot exist.

The application-level checker (`internal/invariants`) still validates all
four invariants with type-aware delta semantics on every run; the schema is
defence in depth behind it, and integration tests exercise the constraints
against real PostgreSQL via testcontainers.

## Consequences

- A double-membership bug anywhere in matching or persistence surfaces as
  an immediate database error, never as a quietly wrong report.
- Schema and checker can only disagree by failing loudly — there is no
  state where the report lies and nothing notices.
