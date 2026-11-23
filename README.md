# Content Platform — Global Creator Subscription Platform

A global, adult-oriented (+18) creator subscription platform: creator pages, free and
exclusive paid content, subscriptions, membership tiers, pay-per-view, tips, messaging,
creator payouts, moderation, age/identity verification and content protection.

**Status:** active development (pre-launch).
**Working title:** "Content Platform". No commercial brand has been approved yet — see
`docs/decisions-log.md`. All brand-adjacent identifiers (Android `applicationId`, package
names, domains) are placeholders until a brand decision is recorded.

## Repository layout

| Path | Purpose |
| --- | --- |
| `docs/` | Architecture, ADRs, database, API, security, compliance, operations, vendor inventory |
| `backend/` | Go 1.27 service: API + workers, PostgreSQL 18/19, migrations |
| `android/` | Native Android client (Kotlin + Jetpack Compose). **Never built locally** — CI only |
| `admin/` | Platform administration console (added in a later phase) |
| `infra/` | Local development compose stack + OpenTofu infrastructure definitions |
| `.github/workflows/` | CI/CD, Android builds, security scanning, release automation |

## Hard engineering rules (non-negotiable)

1. **Android is built only on GitHub-hosted runners.** No Android SDK, Android Studio,
   Gradle or any Android build tooling is installed or executed on the developer
   workstation. See `docs/operations/build-policy.md`.
2. **No secrets in the repository.** All secrets live in GitHub Actions secrets / the
   deployment secret store. `gitleaks` runs on every push.
3. **No real card data ever touches our systems.** Payment vendors are integrated through
   hosted fields / redirect flows (PCI DSS SAQ-A scope).
4. **Money correctness first.** All balances are derived from an append-only double-entry
   ledger. No mutable balance columns.
5. **No feature is claimed complete until it is implemented and proven by tests** that run
   in CI.

## Local development (backend only)

```bash
make dev-db      # start PostgreSQL + Valkey (Docker, no Android tooling)
make migrate     # apply database migrations
make test        # run backend unit + integration tests
make run-api     # start the API server
```

## CI/CD

| Workflow | Trigger | What it does |
| --- | --- | --- |
| `backend-ci.yml` | push / PR touching `backend/**` | format, vet, staticcheck, unit + integration tests (real PostgreSQL service), build binaries, Docker image build (no push) |
| `android-ci.yml` | push / PR touching `android/**` | installs Android SDK on the runner, runs lint + unit tests, assembles debug APK and release AAB, uploads artifacts |
| `security.yml` | push / PR / weekly | gitleaks, `govulncheck`, Trivy filesystem scan, dependency review |
| `release.yml` | tag `v*` | builds signed release artifacts and publishes a GitHub Release |
| `docs.yml` | push touching `docs/**` | Markdown link and formatting checks |

## Ownership and handover

This repository is designed to be transferable: proprietary license, no
personal-account-only dependencies, infrastructure as code, documented operational
runbooks, vendor inventory with costs and contract terms, and an explicit
architecture decision log. See `docs/operations/handover.md`.

## License

Proprietary — all rights reserved. See `LICENSE`.
