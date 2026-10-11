# ADR 0001: Money is BIGINT kuruş, never a float

**Status:** accepted · 2026-07-22

## Context

The engine must represent monetary amounts and discrepancy deltas exactly
across three independent sources. Binary floating point cannot represent
0.10 exactly; accumulated rounding drift across 50K transactions would make
a one-kuruş difference indistinguishable from a reconciliation defect.

## Decision

All monetary amounts are `int64` integer minor units (kuruş) in Go and
`BIGINT` in PostgreSQL. Match scores are basis points, also `int64`, and
tolerance windows are computed in integer arithmetic. An amount becomes a
`TRY` string only at the final rendering step. Any float in a money path
is a defect by definition.

The engine accepts one currency. The currency field must be exactly `TRY`;
a blank, lowercase or space-padded value is rejected. Amounts, gross and
commission are nonnegative, and the direction field (credit or debit)
carries the sign. A marketplace row whose gross minus commission is not its
net amount is rejected too.

The adapters apply these rules per row, so a bad row becomes a rejected row.
`engine.Run` applies them to the whole input and returns an error. It also
rejects duplicate transaction IDs, and a book whose amounts plus commissions
add up to more than `int64` kuruş can hold.

## Consequences

- Invariant checks compare integers exactly; there is no epsilon anywhere.
- `int64` kuruş caps at ~92 quadrillion TRY. No real book gets near that,
  but `engine.Run` checks the book total anyway, and the tolerant threshold,
  basis points and the group subset sum are computed so they cannot
  overflow.
- Currency formatting is a presentation concern, kept in `reporting`.
