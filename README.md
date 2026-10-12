# ReconPilot

[![CI](https://github.com/hilberspace-dev/reconpilot/actions/workflows/ci.yml/badge.svg)](https://github.com/hilberspace-dev/reconpilot/actions/workflows/ci.yml)
[![Open in GitHub Codespaces](https://github.com/codespaces/badge.svg)](https://codespaces.new/hilberspace-dev/reconpilot?quickstart=1)

ReconPilot is a deterministic payment reconciliation engine for e-commerce. It matches transaction records
across three independent sources: a PSP transaction report, a bank statement and a marketplace settlement
(payout) report. Every record it cannot match goes into one of eight discrepancy types, and the engine
refuses to return a result if any input record is left unplaced. When two partners fit a record equally
well it does not pick one: the rows are reported as `ambiguous` for a person to decide.

Correctness checks run in the engine and in PostgreSQL. The engine checks three invariants; the database
makes re-ingesting a file add no transactions. A seeded benchmark compares the engine's output with the
synthetic data's ground truth. This is the verification panel of that benchmark, rendered by
`go run ./cmd/verify` for seed 1:

![Verification panel: PASS, 8/8 injected types detected, 0 false matches, 0 intended pairs or groups missed on 50,359 transactions](docs/verification-panel.png)

## Try it

Without Docker, in a checkout with Go 1.26 or newer:

```sh
go run ./cmd/benchmark          # prints the scored run shown below
go run ./cmd/verify             # writes verification.html, the panel above
```

With Docker, the whole service on the golden dataset (details under "One-command service demo"):

```sh
docker compose up --build -d    # then open http://localhost:8080/report
```

In the browser: the Codespaces badge above opens a Codespace with Go and Docker, builds the project and
runs the benchmark when the terminal attaches.

### Sample input and output

The golden dataset in `testdata/golden/` is twelve rows across three files. The PSP report has seven
orders, the bank statement four credits and the marketplace settlement one payout:

```
psp.csv          ORD-1 100000  ORD-2 250000  ORD-3 50000  ORD-4 80000  ORD-5 120000  ORD-6 64000  ORD-7 99000
bank.csv         ORD-1 100000  ORD-2 249000  ORD-4 68000  ORD-5 120000   (value dates 2 to 9 July)
marketplace.csv  PAY-1 seller-1 gross 114000, commission 17100, net 96900, paid 5 July
```

Amounts are kuruş. The engine turns these rows into three matches and six discrepancies, and the golden
test (`internal/engine/engine_golden_test.go`) asserts exactly these counts:

- ORD-1: the bank credit arrives a day later with the same reference and amount, an exact match.
- ORD-2: the bank credit is 249,000 against 250,000, 0.4 % short and two days later, a tolerant match.
- ORD-3 and ORD-6: 50,000 + 64,000 = 114,000, the gross of PAY-1, a group match; the 17,100 commission is
  booked as one group-level commission record.
- ORD-4: the bank credit is 68,000 against 80,000, 15 % short, inside the 8 to 25 % fee band: a
  commission discrepancy on the PSP row and a delta-0 counterpart record on the bank row.
- ORD-5: the same amount arrives six days later, outside the 3-day window: a timing discrepancy and its
  counterpart record.
- ORD-7: nothing on any other side: missing.

`recon report` renders the same run as HTML and CSV:

![Reconciliation report rendered from the golden dataset](docs/report-screenshot.png)

## Reproducible benchmark

> About 50,000 transactions (`-n 50000`), 3 sources, 8 deliberately injected discrepancy types: 8/8 types detected,
> 0 false matches, 0 intended pairs/groups missed.
> Run it with `go run ./cmd/benchmark`, or read it in a browser after `docker compose up -d verify` at
> [localhost:8081](http://localhost:8081).

Sample output of `go run ./cmd/benchmark` (Go 1.26, seed 1; the elapsed time varies):

```
reconpilot benchmark: n=50359 seed=1
elapsed: 340.7435ms

type           injected   detected    emitted   expected
commission          193        193       1954       1954  ok
refund              386        386        386        386  ok
partial             386        386        772        772  ok
timing              386        386        772        772  ok
duplicate           193        193        193        193  ok
missing             386        386        386        386  ok
ambiguous          1764       1764       1764       1764  ok
unknown             193        193        193        193  ok
(detected: injected records that got their type. expected: the injected records, a delta-0
 record for each commission, partial and timing counterpart, and 1568 group fee records)

clean books: pairs=16548 unreferenced=193 splits=193 reversals=193 groups=1568, matches produced: 19467
false matches: 0, intended pairs/groups not fully matched: 0
runtime invariants: 3/3 PASSED (checked inside engine.Run)

RESULT: PASS, 8/8 injected types detected, 0 false matches, 0 intended pairs/groups missed
```

Each injected record is checked on its own: `detected` counts the injected records that got their type.
`emitted` counts every record of that type the engine produced, and it has to equal `expected`, the number
the book accounts for. The benchmark exits non-zero if an injected record gets another type, if `emitted`
and `expected` differ, if a match is false, or if an intended pair or group is not matched whole. CI runs
it on every push. The PASS line counts only the types the book contains, so a book too small to hold
refunds reports 6/6, and a book with no injected type fails.

The counts change with the seed, but the run passes for every seed from 1 to 100, at the default size
(`-n 50000`) and at `-n 20000`. These two loops check it; they print nothing when every seed passes:

```sh
for s in $(seq 1 100); do go run ./cmd/benchmark -seed "$s" > /dev/null || echo "seed $s failed"; done
for s in $(seq 1 100); do go run ./cmd/benchmark -n 20000 -seed "$s" > /dev/null || echo "seed $s failed"; done
```

`TestScoreHoldsAcrossSeeds` runs seeds 1-30 and 42 at n=50,000 as part of `go test` (it is skipped with
`-short`).

### Benchmark boundary

The data is synthetic and the discrepancies are injected on purpose. The generator and the injection code
are in this repository (`internal/generator`). The benchmark measures how the engine behaves on that data;
its runs on real records are under "Real data" below. Every produced match is checked member by member
against the generator's intended pairing, and the run above found 0 false matches.

The generator also builds the hard cases on purpose, each in a fixed share of the book:

- a clean pair whose bank line has no reference;
- two orders of one seller with the same amount and day, paid by two unreferenced bank lines of that amount;
- a bank line that carries one order's reference and exactly the amount of another order that was never paid;
- an order paid in two or three bank lines;
- a sale voided under its own reference;
- two checks of one amount that both clear late, the first one on the day after the second was issued;
- one payout in twenty with a stray order that lets a second set of orders add up to its gross.

The rows that no evidence can settle are injected as `ambiguous`, and the benchmark fails if the engine
matches any of them. Apart from these cases the open rows of different injections are kept out of each
other's windows, and a seller paid in groups gets at most two payouts 35 days apart. So the benchmark
checks that the engine handles each shape; it does not measure how often such collisions occur in a real
book.

### Real data

The engine has also been run on five public datasets that carry their own ground truth: a bank's
ledger against its statement (BenchRec), marketplace orders against their payments (Olist), a county's
checks against their clearance (Fulton County, Georgia), federal accruals against their payments
(Brazil) and a city's invoices against the checks that paid them (Baltimore). Precision is the share
of the engine's matches that the dataset's own links confirm; the last column is the engine before the rules in
[ADR 0006](docs/adr/0006-match-only-on-sufficient-evidence.md), on the same files.

| Data | Matches | False | Precision | Before |
|---|---|---|---|---|
| Olist | 96,522 | 0 | 100 % | 99.96 % |
| BenchRec, evaluation split | 14,820 | 10 | 99.93 % | 95.64 % |
| Fulton County, bank side without check numbers | 26,320 | 3 | 99.99 % | 98.38 % |
| Brazil | 6,474 | 3 | 99.95 % | 55.56 % |
| Baltimore | 116,089 | 2 | 99.998 % | not finished in 30 minutes |

Precision is not coverage. Where the two sides share no reference, the engine needs the exact amount
and no rival within 30 days, and it leaves the rest open: on the BenchRec evaluation split 37,615 of
69,171 rows are `ambiguous`. Every remaining false match is a coincidence of amounts that the
publisher linked elsewhere: two rows of the same amount, or a set of invoices that adds up to a check. [Runs on public real data](docs/real-data.md) has the conversions,
coverage, each false match and what the runs do not show.

### Verification

- CI ([Actions](https://github.com/hilberspace-dev/reconpilot/actions)) runs on every push: `go vet`, the
  full test suite with the `integration` build tag (property-based tests, and integration tests against a
  real PostgreSQL started by testcontainers), `go-arch-lint`, `govulncheck`, an endpoint smoke test on the
  seeded Compose stack, and a 20K-transaction benchmark with ground-truth checks. The benchmark step of the
  [latest run on `main`](https://github.com/hilberspace-dev/reconpilot/actions?query=branch%3Amain) shows the
  same PASS line on GitHub's own runner.
- Locally, `go run ./cmd/benchmark` is seeded and deterministic. The same command gives the same numbers on
  any machine.
- In a browser, the same scored run is rendered as one self-contained page. It needs no database and no
  seeded book, because the panel generates the synthetic book, runs the engine and scores the outcome
  in-process.

  ```sh
  docker compose up -d verify          # then open http://localhost:8081
  go run ./cmd/verify                  # or write verification.html locally
  go run ./cmd/verify -seed 42         # a different book, to show the result is not seed-specific
  ```

  The panel shows the same four counts for each discrepancy type as the benchmark, says why `expected` is
  above `injected` for `commission`, `partial` and `timing`, and reports false matches and unmatched
  intended pairs. When it writes a file it exits non-zero on the same
  conditions as the benchmark, so it also works as a gate. Both commands score through
  `internal/verification`, which keeps the printed and the rendered result from drifting apart.

## The four correctness guarantees

The engine checks the first three on every run and aborts the run if one of them fails. The fourth belongs
to ingestion: PostgreSQL enforces it, and an integration test runs it against a real database.

| # | Guarantee | Enforced by |
|---|---|---|
| 1 | Every input transaction is either in a match or has a classified discrepancy; no record silently falls out of the result | runtime assertion + property test |
| 2 | No transaction belongs to more than one match group (no double reconciliation) | DB: `UNIQUE(transaction_id)` on `match_member` + assertion |
| 3 | Every transaction-level discrepancy delta obeys its type's money semantics (for example, timing = 0, refund = −full amount, missing = full amount) | runtime assertion + unit/property tests |
| 4 | Re-ingesting the same file adds no transactions | DB: `UNIQUE(dedup_key)` + integration test |

The engine checks guarantee 2, and a schema constraint backs it up. Guarantee 4 is a property of ingestion
that the schema holds; `engine.Run` does not check it. The schema also rejects a discrepancy row that has
no owner. [ADR 0004](docs/adr/0004-schema-level-invariants.md) describes these boundaries and their
integration tests.

## Input rules and limits

- The only currency is TRY. The currency field must be exactly `TRY`; a blank, lowercase or space-padded
  value is rejected.
- Amounts, gross and commission are nonnegative integer kuruş. The direction field (credit or debit)
  carries the sign. For a marketplace row, gross minus commission must equal the net amount.
- An adapter turns a row that breaks these rules into a rejected row. `engine.Run` rejects the whole input
  instead, and it also rejects duplicate transaction IDs and a book whose amounts plus commissions do not
  fit in `int64` kuruş.
- A marketplace payout with more than 20 candidate orders is not searched; it is classified as `unknown`
  ([ADR 0003](docs/adr/0003-bounded-group-matching.md)).
- A bank line may have an empty reference. Leave it empty when the statement prints none: a placeholder
  such as a row number would read as a reference.
- The HTTP API has no authentication (see the service demo below).

[ADR 0001](docs/adr/0001-kurus-bigint-money.md) has the money rules in full.

## One-command service demo

Docker Compose builds the static API image, starts PostgreSQL, loads the golden dataset, runs the
first reconciliation. The API reports readiness on `/readyz`; poll it before sending requests, as CI
does:

```sh
docker compose up --build -d
```

The stack also has a `verify` service on `http://localhost:8081` that serves the verification panel
described above. It depends on nothing else, so `docker compose up -d verify` starts only that service if
all you want is to check the claim.

The API service binds to loopback by default at `http://localhost:8080`:

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/healthz` | Process liveness |
| `GET` | `/readyz` | PostgreSQL readiness |
| `POST` | `/api/v1/reconciliation-runs` | Recompute, check runtime invariants, persist, return JSON summary |
| `GET` | `/report` | Server-rendered HTML report |
| `GET` | `/metrics` | Prometheus-compatible metrics |

```sh
curl http://localhost:8080/readyz
curl -X POST http://localhost:8080/api/v1/reconciliation-runs
curl http://localhost:8080/metrics
# Open http://localhost:8080/report in a browser
```

The Compose credentials are local-demo defaults, the API is intentionally unauthenticated, and the
PostgreSQL data lives in a named volume. Keep the demo bound to loopback. Add authentication,
authorization, secret management and TLS before you expose it outside a local machine.

## CLI quickstart

Requirements: Go 1.26+, Docker.

```sh
# 1. Start PostgreSQL
docker run --name reconpilot-pg -e POSTGRES_PASSWORD=postgres -e POSTGRES_DB=reconpilot \
  -p 5432:5432 -d postgres:17-alpine
export DATABASE_URL="postgres://postgres:postgres@localhost:5432/reconpilot"

# 2. Ingest the three golden source files (a second run adds no transactions)
go run ./cmd/recon ingest -source psp         -file testdata/golden/psp.csv
go run ./cmd/recon ingest -source bank        -file testdata/golden/bank.csv
go run ./cmd/recon ingest -source marketplace -file testdata/golden/marketplace.csv

# 3. Reconcile (exact → split → tolerant → group → tolerant again, then classification + invariant check)
go run ./cmd/recon run

# 4. Render the report
go run ./cmd/recon report        # writes reports/report.html + reports/discrepancies.csv
```

The benchmark runs in memory, so reproducing the benchmark needs no database:

```sh
go run ./cmd/benchmark           # flags: -n <count> -seed <seed>
```

Tests: `go test ./...` runs without Docker. The integration tests carry the `integration` build tag and
start a real PostgreSQL through testcontainers, so `go test -tags integration ./...` needs Docker running.
CI runs the tagged form.

```sh
go test ./... -count=1                       # no Docker needed
go test -tags integration ./... -count=1     # adds the PostgreSQL integration tests; needs Docker
go tool go-arch-lint check                   # architecture boundaries, pinned via the go.mod tool directive
```

## How it works

```
        CSV (psp / bank / marketplace)
                    │
      ingestion          poison-row rejection, sha256 dedup_key
                    │
                  store            PostgreSQL: schema-level invariants
                    │                         ┌──────────────────────────────┐
                    ├─────────────────────────┤ REST · HTML · health · metrics │
                    │                         └──────────────────────────────┘
                 engine.Run ──────────────── pure, in-memory, deterministic
                    │
        ┌───────────┼───────────────┐
   exact · split · tolerant · group      matching chain, in order
        └───────────┼───────────────┘
                    │
             classification        8 discrepancy types
                    │
              invariants.Check     runtime checks 1–3, hard-failing
                    │
                reporting          HTML + CSV, ageing buckets
```

- The matching chain is deterministic and explainable, and it runs in this order:
  1. `exact`: reference + amount + same direction, value dates within ±3 days. A row without a reference
     never exact-matches. A same-reference payment that arrives later than that is reported as a
     *timing* discrepancy.
  2. `split`: one PSP row and two or more bank lines with its reference that add up to its amount exactly,
     all within ±3 days (an order paid by voucher and card, say).
  3. `tolerant`: bank lines only. With the same reference the amount may differ by up to 0.5 %. Without
     one the amount must be exact and the pair must be the only fit: no other open bank line of that
     counterparty and amount within 30 days of the PSP row, and no other open PSP row within 30 days of
     the bank line. The same pass pairs a PSP sale with its void when both carry the same reference
     (`reversal`). Marketplace payouts go to the group matcher and are never tolerant-matched.
  4. `group`: many-to-one (payout = Σ orders − commission) with a bounded subset search. Candidates are
     narrowed by counterparty and a 14-day window, and a payout with more than 20 candidates is not
     searched; it becomes an explicit `unknown` ([ADR 0003](docs/adr/0003-bounded-group-matching.md)).
     If two different sets of orders add up to the gross, neither is matched, and a later payout whose
     set would use one of those orders is refused as well. Payouts are taken in date order, and payouts
     of one day in reference order.
- After the group pass the tolerant pass runs once more over the open rows, because an order the group
  pass took no longer competes for a bank line.
- A reference identifies a record within one counterparty. A row whose reference names a record on the
  other side of the book never pairs with a row that carries a different reference. Rows left open on
  purpose (more than one partner fits, or the only partner carries too little evidence) are classified
  `ambiguous` ([ADR 0006](docs/adr/0006-match-only-on-sufficient-evidence.md)).
- Classification has 8 types: commission deduction, refund, partial payment, timing shift, duplicate record,
  missing on counterparty side, ambiguous, and unknown difference. The rules run in a fixed order on a sorted copy of
  the input, so the result does not depend on input order.
- Money is `int64` kuruş everywhere, and a float in a money path is a defect by definition
  ([ADR 0001](docs/adr/0001-kurus-bigint-money.md)).
- It is one Go module with no framework, just the standard library and `pgx`. The service is one static
  binary (`recon`); a second small binary (`verify`) serves the benchmark panel.
  `go-arch-lint` in CI enforces the one-way package flow
  `ingestion → matching → classification → reporting`, so a boundary violation fails the build
  ([ADR 0002](docs/adr/0002-single-service-single-stack.md)).
- Matching and classification are pure in-memory code: a functional core inside an imperative shell. That
  is why the property-based tests (100 randomized books per run, `pgregory.net/rapid` with shrinking) are
  cheap and deterministic.

## Adding a source adapter

Each source is one file in `internal/ingestion/` (see `psp.go`, `bank.go`, `marketplace.go`):
implement a header check and a row-to-`domain.Transaction` mapping, and register it in `AdapterFor`.
A bad row becomes a `RejectedRow` and never aborts the batch. Deduplication is the
store's job; the adapter only parses. Two lines of one file that agree on every keyed field (two equal
unreferenced payments on one day, say) are both kept: the second gets an occurrence key, and reading the
same file again gives the same keys.

`ingestion.Ingest` parses the whole file before it touches the database. It then stores the batch, its
transactions, its rejected rows and the batch counts (`status = 'ingested'`, `row_count`,
`rejected_count`) in one database transaction. A file with a bad header stores nothing.

## Roadmap

- Done: the stdlib HTTP surface in [ADR 0005](docs/adr/0005-stdlib-http-surface.md) (REST API,
  server-rendered report, liveness and readiness, Prometheus-compatible `/metrics`, graceful shutdown),
  a minimal container image, and a one-command PostgreSQL stack with the seeded demo.
- Next: a PSP sandbox adapter as an integration proof, and a small operator dashboard.
- Next: references read out of bank narrative text. Without them most rows of an unreferenced bank
  statement stay `ambiguous` (see `docs/real-data.md`).
- Later: group matching beyond 20 members (smarter pruning or MILP).

## Design docs

- [ADR 0001: Money is BIGINT kuruş, never a float](docs/adr/0001-kurus-bigint-money.md)
- [ADR 0002: One Go module, one service binary, no framework](docs/adr/0002-single-service-single-stack.md)
- [ADR 0003: Bounded many-to-one matching with an explicit fallback](docs/adr/0003-bounded-group-matching.md)
- [ADR 0004: Invariants enforced at the schema level](docs/adr/0004-schema-level-invariants.md)
- [ADR 0005: Stdlib HTTP surface and bounded observability](docs/adr/0005-stdlib-http-surface.md)
- [ADR 0006: Match only when the evidence singles out one partner](docs/adr/0006-match-only-on-sufficient-evidence.md)
- [Case study](docs/case-study.md)
