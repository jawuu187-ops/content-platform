# Decision Log

Running record of product and engineering decisions. Architecture decisions with
significant, hard-to-reverse consequences live as ADRs in `docs/architecture/adr/`.

| ID | Date | Decision | Status | Notes |
| --- | --- | --- | --- | --- |
| D-001 | 2026-11-23 | Product scope: adult (+18) creator subscription platform, non-explicit nudity only in paid surfaces | Accepted | Content policy fixed; see `docs/compliance/content-policy.md` |
| D-002 | 2026-11-23 | Native Android (Kotlin + Compose) as the first client | Accepted | ADR-0001 |
| D-003 | 2026-11-23 | Go modular monolith backend; services extracted only on measured need | Accepted | ADR-0002 |
| D-004 | 2026-11-23 | PostgreSQL as system of record; append-only double-entry ledger for money | Accepted | ADR-0003, ADR-0004 |
| D-005 | 2026-11-23 | Android built exclusively on GitHub-hosted runners; no local Android toolchain | Accepted | Build policy; ADR-0005 |
| D-006 | 2026-11-23 | No commercial brand approved yet; all identifiers are placeholders | Open | Brand decision required before store submission |
| D-007 | 2026-11-23 | Payment strategy must support cards **and** crypto/prepaid wallet balances | Accepted | ADR-0006; vendor inventory in `docs/vendors/` |
| D-008 | 2026-11-23 | Prepaid wallet balance is the primary mechanism enabling recurring billing in crypto-only markets | Accepted | ADR-0006 |
| D-009 | 2026-11-23 | Monetisation engineering target: tiered platform commission (not a flat 20 %) | Accepted | Economics documented in `docs/vendors/economics.md` |
| D-010 | 2026-11-23 | OWASP ASVS 5.0 as the security verification baseline | Accepted | `docs/security/threat-model.md` |
| D-011 | 2026-11-23 | Copyleft dependencies are not permitted in product code paths | Accepted | ADR-0010 |
| D-012 | 2026-11-23 | Repository is designed for handover/acquisition from day one | Accepted | `docs/operations/handover.md` |
| D-013 | 2026-11-23 | Android distribution is direct APK/AAB + PWA; **no** adult-content listing in Play/App Store (store policy is a hard block) | Proposed — needs owner approval | `docs/plan/01-product-and-architecture.md` §1.5, `docs/vendors/payments-and-distribution.md` §5 |
| D-014 | 2026-11-23 | Payment strategy: two card MoRs applied for in parallel (CCBill + Segpay), crypto + prepaid wallet as second rail, payouts via Paxum + fiat fallback, all behind one adapter interface | Proposed — owner must sign contracts | `docs/vendors/payments-and-distribution.md` §2–4 |
| D-015 | 2026-11-23 | Delivery is staged in 13 phases (P0–P12) with hard exit gates; no phase is "done" without CI-proven tests | Accepted | `docs/roadmap.md`, `docs/plan/02-delivery-timeline-cost-risk.md` |
| D-016 | 2026-11-23 | Age assurance (HEAA-class) is enforced from P2 and gated at the entitlement layer, not at the UI | Accepted | `docs/plan/01-product-and-architecture.md` §2.7 |
| D-017 | 2026-11-23 | Actual payment vendor terms must be verified in writing before integration code is written; no vendor assumed accepted | Accepted | `docs/vendors/payments-and-distribution.md` §7 |
| D-018 | 2026-11-23 | Private repo on the free plan: branch protection and CodeQL are unavailable (Pro/GHAS only); gosec + staticcheck + govulncheck + gitleaks replace CodeQL until the owner upgrades | Accepted | `docs/operations/ci.md` §2 |
| D-019 | 2026-11-23 | Toolchain is pinned and advances only after a green CI run on a branch: Go 1.27, PG 18, AGP 8.13.2, Gradle 8.14.3, Kotlin 2.2.20, Compose BOM 2026.05.01, compileSdk 36 | Accepted | `docs/operations/ci.md` §3 |
| D-020 | 2026-11-23 | Android builds run only on GitHub-hosted runners and the produced APK/AAB are the acceptance evidence for the client | Accepted | ADR-0005, `docs/operations/ci.md` §4 |
