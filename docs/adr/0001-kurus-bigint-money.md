# ADR 0001 — Money is BIGINT kuruş, never a float

**Status:** accepted · 2026-07-22

## Context

The entire purpose of this engine is to prove that money balances to the
kuruş across three independent sources. Binary floating point cannot
represent 0.10 exactly; accumulated rounding drift across 50K transactions
would make the central claim — "zero kuruş imbalance" — unprovable, and any
observed imbalance indistinguishable from a real reconciliation bug.

## Decision

All monetary amounts are `int64` integer minor units (kuruş) in Go and
`BIGINT` in PostgreSQL. Match scores use basis points (`int64`), tolerance
windows are computed in integer arithmetic, and amounts are formatted into
`TRY` strings only at the final rendering step. Any float in a money path
is a defect by definition.

## Consequences

- Invariant checks compare integers exactly; there is no epsilon anywhere.
- `int64` kuruş caps at ~92 quadrillion TRY — no realistic overflow.
- Currency formatting is a presentation concern, isolated in `reporting`.
