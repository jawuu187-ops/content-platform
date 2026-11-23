# ADR-0002 — Go modular monolith with extractable services

**Status:** Accepted — 2026-11-23

## Context
The platform needs payments-grade correctness, low tail latency, cheap horizontal scaling,
and a team small enough that operational surface area is a real constraint. Services must
be separable later without a rewrite.

## Decision
One Go module organised by bounded context (`internal/identity`, `internal/billing`, …),
two deployables (`cmd/api`, `cmd/worker`), each context owning its schema and exposing a
Go interface. Extraction to a separate service happens only when a measured trigger is hit
(blast radius, wildly different scaling profile, compliance boundary).

## Alternatives considered
| Option | Assessment |
| --- | --- |
| Node.js 26 LTS + NestJS | Fast to write; weaker CPU-bound behaviour and more runtime variance on the money path. |
| Java 25 LTS + Spring Boot | Very mature; higher memory footprint and slower iteration for this team size. |
| Rust (axum) | Best resource safety; development velocity and hiring pool are not competitive for a product that must ship. |
| Microservices from day one | Premature: distributed transactions across money paths without a proven scaling need. |

## Consequences
- Single binary, small containers, predictable concurrency, strong standard-library HTTP.
- Fewer batteries included than Spring/Nest: we assemble our own HTTP, config, migration
  and validation layers (see `internal/platform`).
- Module boundaries are enforced by review and by a dependency check in CI.
