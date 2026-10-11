# ADR 0002: One Go module, one service binary, no framework

**Status:** accepted · 2026-07-22

**Amendment:** [ADR 0005](0005-stdlib-http-surface.md) adds the HTTP and observability surface inside the
same binary. The single-service decision is unchanged.

## Context

Several deployables in different languages would multiply the integration surface and the CI time. In a
project that lives on correctness claims, they would also multiply the places where a claim can break without
anyone noticing.

## Decision

The engine is one Go module and the service is one static binary, `recon`. A second small binary, `verify`,
came later for the benchmark panel. The standard library covers CSV, HTML templating and logging; the only
runtime dependency is `pgx` for PostgreSQL. In CI, `go-arch-lint` (pinned through Go's `tool` directive) checks
the architecture boundaries inside the module, so a change that breaks the one-way flow
`ingestion → matching → classification → reporting` fails the build. The core is pure in-memory computation;
PostgreSQL and the CLI are the imperative shell around it.

## Consequences

- `go build ./cmd/recon` produces the service. So a deploy is one file plus Postgres.
- Because the core is pure, property-based tests are cheap and deterministic.
- REST and UI layers waited until the CLI and the benchmark had settled the correctness core. ADR 0005 then
  added that surface as another imperative shell around the same engine.
