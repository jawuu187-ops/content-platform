# ADR-0010 — Dependency and license policy

**Status:** Accepted — 2026-11-23

## Context
The repository must be sellable/transferable without a license audit surprise.

## Decision
1. Product code paths must not depend on copyleft licenses (GPL/AGPL/SSPL) or on licenses
   that restrict commercial use in a way that could encumber a transfer (BUSL for services
   we might resell).
2. Permissive licenses (MIT, Apache-2.0, BSD, ISC, MPL-2.0) are preferred.
3. Every dependency must be declared in the lockfile and reported by CI with its license
   identifier. A CI gate fails the build when a new copyleft license enters a product path
   without an explicit, recorded exemption.
4. Vendored or self-hosted infrastructure components follow the same rule where they are
   distributed with the product.
5. No dependency may be added to acquire a single trivial function.

## Consequences
- Known exclusions from consideration: GPL-licensed search engine (Typesense) in product
  paths; AGPL/Valkey-style license churn is avoided by choosing BSD-licensed equivalents.
- Infrastructure choices favour Linux Foundation / Apache-2.0 projects (e.g. OpenTofu,
  OpenBao, Valkey, Kubernetes, Kafka, Argo CD, OpenTelemetry).
