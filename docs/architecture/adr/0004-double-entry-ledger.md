# ADR-0004 — Append-only double-entry ledger for all money

**Status:** Accepted — 2026-11-23

## Context
The platform holds creator earnings, applies commissions, processes refunds and
chargebacks, and pays out. Mutable balance columns and ad-hoc arithmetic are the standard
cause of unreconcilable balances and lost creator trust in this industry.

## Decision
All money is represented as immutable journal entries. Every journal must balance to zero
across accounts. Account types: `PLATFORM_REVENUE`, `CREATOR_PAYABLE`, `CREATOR_RESERVE`,
`FAN_WALLET`, `PROCESSOR_CLEARING`, `CHARGEBACK_EXPENSE`, `PAYOUT_CLEARING`, `TAX_PAYABLE`.
Balances are projections derived by aggregation; no `UPDATE ... SET balance = ...` exists
anywhere in the codebase.

## Rules
1. Integer minor units only (`BIGINT`), with an explicit currency. No floating point.
2. Each journal is idempotent under a caller-supplied idempotency key.
3. Entries are append-only; corrections are new reversing journals, never edits.
4. Every journal records its source (`payment`, `refund`, `payout`, `adjustment`, …) and the
   external reference of the originating event.
5. Daily automated reconciliation compares ledger against processor settlement reports and
   opens a case on any variance.
6. Adjustments require a reason, an actor, and four-eyes approval above a threshold.

## Consequences
- Correctness is testable: property-based tests assert global balance invariants.
- Higher storage volume (accepted) and the need for snapshotting for fast balance reads.
- Phase 2 may move the hot path to a specialised ledger engine; the interface stays.
