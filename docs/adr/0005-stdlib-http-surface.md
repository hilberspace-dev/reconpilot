# ADR 0005: Stdlib HTTP surface and bounded observability

**Status:** accepted · 2026-07-22

## Context

The deterministic engine and the CLI cover the matching core. But an operational backend also needs a
network surface that stays stable, a readiness check that reflects the database, machine-readable telemetry
and a repeatable deployment. None of that may pull matching or classification rules into HTTP handlers.

## Decision

- Serve HTTP from the existing `recon` binary with `net/http`. No separate API service, no web framework.
- One versioned mutation endpoint, `POST /api/v1/reconciliation-runs`: it loads the persisted transactions,
  calls `engine.Run`, then persists the result atomically through the existing store boundary.
- One reconciliation run per process at a time. A concurrent request gets `409 Conflict`; two runs can then
  never race on the replace-in-place persistence transaction.
- `/healthz` answers for process liveness and `/readyz` for a bounded PostgreSQL ping. The existing HTML report
  is served at `/report`, Prometheus text exposition at `/metrics`.
- Metric names and route labels are fixed. A transaction, a merchant, a filename or any other unbounded or
  user-controlled value never becomes a label.
- The server runs with read and write timeouts and shuts down gracefully on SIGINT and SIGTERM.
- The binary ships in a non-root `scratch` image, with exact Go and Alpine builder tags and exact PostgreSQL
  patch and base tags. The local Compose stack starts PostgreSQL, seeds the golden dataset (seeding again adds
  no transactions) and runs the first reconciliation; after that the API reports ready on `/readyz`.

## Consequences

- The HTTP layer is an imperative shell around the same pure engine the CLI and the tests use.
- Operational checks and metrics need no runtime dependency beyond `pgx`.
- The REST surface is small on purpose. Source upload, authentication and a richer operator UI are separate
  product decisions; this one does not cover them.
- The Compose API is an unauthenticated demo bound to loopback, not a template for anything internet-facing.
