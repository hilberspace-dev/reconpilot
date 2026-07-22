# ReconPilot

[![CI](https://github.com/hilberspace-dev/reconpilot/actions/workflows/ci.yml/badge.svg)](https://github.com/hilberspace-dev/reconpilot/actions/workflows/ci.yml)

A deterministic payment reconciliation engine for e-commerce: it matches transaction records
across three independent sources — a **PSP transaction report**, a **bank statement**, and a
**marketplace settlement (payout) report** — classifies every record it cannot match into one of
seven discrepancy types, and proves that not a single kuruş went missing along the way.

Correctness is not claimed in prose. It is enforced by **runtime invariants**, exercised by
**property-based tests**, and demonstrated by a **reproducible benchmark anyone can run**.

## The claim

> **50,000 transactions · 3 sources · 7 deliberately injected discrepancy types →
> 7/7 types detected, 0 false matches, zero kuruş imbalance.
> Run it yourself: `go run ./cmd/benchmark`**

**Honesty rule.** The data is synthetic and the discrepancies are deliberately injected — the
generator and the injection code live in this repository (`internal/generator`). The claim is
*not* "this ran on real customer data"; it is **"the engine's behaviour is verifiable, and you
can verify it yourself in one command."** Every match the engine produces is checked
member-by-member against the generator's intended-pairing ground truth, so "0 false matches" is
a **measured number**, not an assertion.

Output of `go run ./cmd/benchmark` on this machine (Go 1.26, seed 1):

```
reconpilot benchmark — n=49003 seed=1
elapsed: 3.2393039s

type           injected   detected
commission          193       2036  ok
refund              386        386  ok
partial             193        386  ok
timing              193        386  ok
duplicate           193        193  ok
missing             193        193  ok
unknown             193        193  ok
(commission 'detected' includes one fee record per clean marketplace group: 1650 groups)

clean books: pairs=17706 groups=1650 — matches produced: 20128
false matches: 0 — intended pairs/groups not fully matched: 0
invariants: all 4 PASSED (checked inside engine.Run)

RESULT: PASS — 7/7 injected types detected, 0 false matches, kuruş balance intact
```

The benchmark exits non-zero if any injected type goes undetected, any false match is produced,
or any intended pair/group is left unmatched — CI runs it on every push.

### Where to verify, independently of anything this README says

- **CI** ([Actions](https://github.com/hilberspace-dev/reconpilot/actions)) — every push re-runs
  `go vet`, the full test suite (property-based + integration against a real PostgreSQL via
  testcontainers), `go-arch-lint`, `govulncheck`, and a 20K-transaction benchmark with
  ground-truth validation.
- **Locally** — `go run ./cmd/benchmark` is seeded and deterministic; the same command, the
  same numbers, on any machine.
- **History** — the commit log is incremental (one component per commit), and every
  load-bearing decision has an [ADR](docs/adr/).

## The four invariants

A violation of any of these is a hard failure — the run aborts, never a warning.

| # | Invariant | Enforced by |
|---|---|---|
| 1 | Every kuruş is either inside a match or inside a classified discrepancy — money is never lost | runtime assertion + property test |
| 2 | No transaction belongs to more than one match group — no double reconciliation | **DB: `UNIQUE(transaction_id)` on `match_member`** + assertion |
| 3 | Σ(classified discrepancy deltas) equals the reported total difference — the report cannot lie | runtime assertion + property test |
| 4 | Re-ingesting the same file is a no-op | **DB: `UNIQUE(dedup_key)`** + integration test |

Invariants 2 and 4 are guaranteed at the **schema** level in addition to the application level —
deliberate defence-in-depth: application logic can be refactored incorrectly; a database
constraint cannot be bypassed by a code path that forgot about it
([ADR 0004](docs/adr/0004-schema-level-invariants.md)).

## Quickstart

Requirements: Go 1.26+, Docker.

```sh
# 1. Start PostgreSQL
docker run --name reconpilot-pg -e POSTGRES_PASSWORD=postgres -e POSTGRES_DB=reconpilot \
  -p 5432:5432 -d postgres:17-alpine
export DATABASE_URL="postgres://postgres:postgres@localhost:5432/reconpilot"

# 2. Ingest the three golden source files (idempotent — run twice, second run dedupes)
go run ./cmd/recon ingest -source psp         -file testdata/golden/psp.csv
go run ./cmd/recon ingest -source bank        -file testdata/golden/bank.csv
go run ./cmd/recon ingest -source marketplace -file testdata/golden/marketplace.csv

# 3. Reconcile (exact → tolerant → group matching, then classification + invariant check)
go run ./cmd/recon run

# 4. Render the report
go run ./cmd/recon report        # writes reports/report.html + reports/discrepancies.csv
```

The report rendered from the golden dataset — 12 transactions, 3 matches (one per matcher),
6 classified discrepancies, zero `unknown`:

![Reconciliation report rendered from the golden dataset](docs/report-screenshot.png)

To reproduce the 50K claim, no database is needed — the benchmark is pure in-memory:

```sh
go run ./cmd/benchmark           # flags: -n <count> -seed <seed>
```

Tests (integration tests start a real PostgreSQL via testcontainers — Docker must be running):

```sh
go test ./... -count=1
go tool go-arch-lint check       # architecture boundaries, pinned via the go.mod tool directive
```

## How it works

```
        CSV (psp / bank / marketplace)
                    │
                ingestion          poison-row rejection, sha256 dedup_key
                    │
                  store            PostgreSQL: schema-level invariants
                    │
                 engine.Run ──────────────── pure, in-memory, deterministic
                    │
        ┌───────────┼───────────────┐
      exact      tolerant         group      matching chain, in order
        └───────────┼───────────────┘
                    │
             classification        7 discrepancy types
                    │
              invariants.Check     all 4, hard-failing
                    │
                reporting          HTML + CSV, ageing buckets
```

- **Matching chain** (deterministic and explainable, in order):
  1. `exact` — reference + amount + same direction, value dates within ±3 days (a same-reference
     payment arriving later than that is a *timing* discrepancy to surface, not a silent match);
  2. `tolerant` — ±0.5% amount, ±3 days, **bank lines only** (marketplace payouts are
     group-matched, never tolerant-matched);
  3. `group` — many-to-one (payout = Σ orders − commission), bounded subset search: candidates
     narrowed by counterparty + 14-day window, group size capped at ≤20 members; anything past
     the bound degrades honestly to `unknown`
     ([ADR 0003](docs/adr/0003-bounded-group-matching.md)).
- **Classification — 7 types:** commission deduction · refund · partial payment · timing shift ·
  duplicate record · missing on counterparty side · unknown difference.
- **Money is `int64` kuruş everywhere** — a float in a money path is a defect by definition
  ([ADR 0001](docs/adr/0001-kurus-bigint-money.md)).
- **One Go service, one static binary, no framework** — stdlib + `pgx`; the one-way package flow
  `ingestion → matching → classification → reporting` is enforced by `go-arch-lint` in CI, so a
  boundary violation is a build failure, not a code-review opinion
  ([ADR 0002](docs/adr/0002-single-service-single-stack.md)).
- **Functional core / imperative shell:** the entire matching + classification pipeline is pure
  in-memory computation, which is what makes property-based testing (100 randomized books per
  run, `pgregory.net/rapid` with shrinking) cheap and deterministic.

## Adding a source adapter

Each source is one file in `internal/ingestion/` (see `psp.go`, `bank.go`, `marketplace.go`):
implement a header check and a row-to-`domain.Transaction` mapping, and register it in
`AdapterFor`. Poison rows are collected per-row (`RejectedRow`), never abort the batch; dedup and
idempotency come from the store, not the adapter.

## Roadmap

- **Phase 2 — visibility layer:** server-rendered report UI in the same service (stdlib
  `html/template`), REST API, PSP sandbox adapter as a liveness proof, Prometheus `/metrics`,
  live demo deploy (single binary → one small VPS).
- **Phase 3 — AI assist (optional, feature-flagged):** LLM-generated plain-language discrepancy
  narratives and resolution suggestions via `anthropic-sdk-go`, strictly human-in-the-loop — the
  correctness claim never depends on a model.
- Larger-than-20-member group matching (smarter pruning / MILP) — stated openly as future work.

## Design docs

- [ADR 0001 — Money is BIGINT kuruş, never a float](docs/adr/0001-kurus-bigint-money.md)
- [ADR 0002 — One Go service, one static binary, no framework](docs/adr/0002-single-service-single-stack.md)
- [ADR 0003 — Bounded many-to-one matching with an explicit fallback](docs/adr/0003-bounded-group-matching.md)
- [ADR 0004 — Invariants enforced at the schema level](docs/adr/0004-schema-level-invariants.md)
- [Case study](docs/case-study.md)
