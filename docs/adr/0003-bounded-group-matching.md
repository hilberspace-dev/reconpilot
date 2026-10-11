# ADR 0003: Bounded many-to-one matching with an explicit fallback

**Status:** accepted · 2026-07-22

## Context

Marketplace settlement is many-to-one: one payout covers a set of orders
(gross = Σ orders, minus commission). Finding that set is a subset-sum-like
search with no safe general worst case. An unbounded search could consume
the run, or worse, push the design toward "best effort" heuristics whose
failures nobody sees.

## Decision

The search is bounded before it starts. Candidates are narrowed to the
payout's counterparty within a 14-day payout window, and only positive
credit lines qualify as addends. A payout with more than 20 candidate orders
is not searched at all: it goes on the group-candidate list and is
classified `unknown`. Within the bound, deterministic backtracking visits
candidates in chronological order (oldest orders settle first), so the
chosen subset never depends on input ordering.

## Consequences

- The worst case is capped at 2^20 combinations. The benchmark's payouts
  cover 2 to 8 orders each, so its searches stay far smaller than that.
- A payout over the bound becomes an explicit `unknown`. Every invariant
  still holds, and the report shows that payout as unknown.
- Matching larger groups is on the README roadmap.
