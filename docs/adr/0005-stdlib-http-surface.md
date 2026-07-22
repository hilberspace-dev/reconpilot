# ADR 0005 — Stdlib HTTP surface and bounded observability

**Status:** Accepted

**Date:** 2026-07-22

## Context

The deterministic engine and CLI prove the matching core, but an operational backend also needs a
stable network surface, dependency-aware readiness, machine-readable telemetry, and repeatable
deployment. These concerns must not move matching or classification rules into handlers.

## Decision

- Keep one binary and use `net/http`; no second service or web framework.
- Expose one versioned mutation endpoint:
  `POST /api/v1/reconciliation-runs`. It loads persisted transactions, calls `engine.Run`, then
  atomically persists the result through the existing store boundary.
- Permit one reconciliation run per process at a time; a concurrent request receives `409 Conflict`
  instead of racing the replace-in-place persistence transaction.
- Expose `/healthz` for process liveness and `/readyz` for a bounded PostgreSQL ping.
- Expose the existing HTML report at `/report` and Prometheus text exposition at `/metrics`.
- Use fixed metric names and fixed route labels. No transaction, merchant, filename, or other
  unbounded/user-controlled value may become a label.
- Run the server with read/write timeouts and graceful SIGINT/SIGTERM shutdown.
- Package the binary in a non-root `scratch` image, with exact Go/Alpine builder and PostgreSQL
  patch/base tags. The local Compose stack starts PostgreSQL, idempotently seeds the golden dataset,
  and waits for readiness.

## Consequences

- The HTTP layer is an imperative shell around the same pure engine exercised by the CLI and tests.
- Operational checks and metrics are available without adding a runtime dependency beyond `pgx`.
- The REST surface is intentionally small; source upload/authentication and a richer operator UI
  remain separate product decisions rather than being improvised into this foundation.
- The Compose API is an unauthenticated loopback demo, not an internet-facing deployment template.
