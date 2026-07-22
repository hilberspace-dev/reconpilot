# ADR 0003 — Bounded many-to-one matching with an explicit fallback

**Status:** accepted · 2026-07-22

## Context

Marketplace settlement is many-to-one: one payout covers a set of orders
(gross = Σ orders, minus commission). Finding that set is a subset-sum-like
search with no safe general worst case; an unbounded search could consume
the run — or worse, force "best effort" heuristics whose failures are
invisible.

## Decision

The search is bounded before it starts: candidates are narrowed to the
payout's counterparty within a 14-day payout window, only positive credit
lines qualify as addends, and candidate sets larger than 20 members are not
searched at all — the payout is marked `group_candidate` and classified
`unknown`. Within the bound, deterministic backtracking visits candidates
in chronological order (oldest orders settle first), so the chosen subset
never depends on input ordering.

## Consequences

- The worst case is capped at 2^20 nodes and in practice pruned far below.
- Overflow degrades to an explicit `unknown` — every invariant still holds,
  and the report says what the engine could not do rather than guessing.
- Larger-group matching is roadmap work, stated openly in the README.
