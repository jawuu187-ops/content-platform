# ADR-0003 — PostgreSQL as the system of record

**Status:** Accepted — 2026-11-23

## Context
We need strong transactional guarantees across identity, entitlements and money; rich
querying for analytics and operations; predictable operations; and the ability to scale to
millions of users without re-platforming.

## Decision
PostgreSQL (18/19) is the single system of record. Read replicas for read-heavy paths,
PgBouncer for connection multiplexing, table partitioning for append-only high-volume
tables, `pgvector` for embeddings where useful, PITR backups.

## Alternatives considered
| Option | Assessment |
| --- | --- |
| CockroachDB / YugabyteDB | Real multi-region active-active writes; cost and operational complexity not justified before there is a measured need. |
| MySQL / Vitess | Viable at extreme write scale; weaker feature set for our analytical and JSON/reporting needs. |
| MongoDB | Entitlement and ledger integrity benefit from relational constraints and joins. |
| Sharded Postgres from day one | Premature; shard key is `creator_id` and can be introduced later behind a repository layer. |

## Consequences
- Referential integrity is a feature we rely on for money correctness.
- Horizontal write scaling is deferred and planned (shard by `creator_id`, move ledger hot
  path to a specialised ledger engine if volume demands).
