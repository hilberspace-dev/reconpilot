# ADR 0002 — One Go service, one static binary, no framework

**Status:** accepted · 2026-07-22

## Context

Earlier drafts of this system had three deployables (JVM core, Python LLM
service, React SPA). Every additional stack multiplies integration surface,
CI time, and — for a correctness-first project — the number of places a
claim can silently break. A heavyweight framework would also write a large
share of the code, which weakens the author's ability to defend every line.

## Decision

The engine is a single Go module producing a single static binary. The
standard library covers CSV, HTML templating and logging; the only runtime
dependency is `pgx` (PostgreSQL). Architecture boundaries inside the module
are enforced by `go-arch-lint` in CI (pinned via Go's `tool` directive), so
the one-way flow `ingestion → matching → classification → reporting` is a
build failure, not a code-review opinion. The core is pure in-memory
computation (functional core); PostgreSQL and the CLI form the imperative
shell.

## Consequences

- `go build ./...` produces everything; deploys are one file plus Postgres.
- The pure core makes property-based testing cheap and deterministic.
- REST/UI layers are deliberately deferred (Phase 2) — the CLI plus the
  benchmark is what proves correctness.
