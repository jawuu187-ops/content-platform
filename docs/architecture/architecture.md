# Architecture

## 1. Shape

A **modular monolith in Go** with explicit bounded contexts, one deployable API and one
deployable worker fleet, plus separate front-ends. Services are extracted only when a
measured reason exists (blast radius, scaling profile, compliance boundary) — never for
fashion. Every module has a public Go interface, its own database schema, and no direct
access to another module's tables except through its interface.

```
Android (Kotlin/Compose)   Web/PWA (SFW + adult)   Admin console (isolated)
        |                          |                        |
        +------------ Cloudflare (WAF, bot mgmt, rate limit, DDoS) ---------+
                                    |
                            Edge / API gateway (Go)
                                    |
   +----------------+---------------+---------------+------------------+
   |  identity      |  content      |  media        |  moderation      |
   |  catalog       |  subscriptions|  billing      |  ledger          |
   |  payouts       |  messaging    |  notifications|  analytics       |
   +----------------+---------------+---------------+------------------+
                                    |
      PostgreSQL 18+ (partitioned, replicas, PITR)   Valkey (cache, limits)
      Object storage (masters + renditions)          Event bus + outbox
      ClickHouse (analytics, reconciliation)         Temporal (durable money flows)
                                    |
                     Video path: resumable upload -> transcode -> package
                     (HLS) -> signed short-lived delivery + watermark
```

## 2. Non-negotiable principles

1. **Money has one source of truth.** An append-only double-entry ledger. Balances are
   projections, never mutable columns. Every movement is idempotent and traceable to an
   external event or a user action.
2. **Transactions before messages.** No side effect is dispatched outside a database
   transaction; events are written to an outbox table and published after commit.
3. **Media never flows through the API.** Direct-to-storage resumable upload with signed
   URLs; delivery from the CDN with a short-lived token.
4. **Content is private by default.** Every asset requires an explicit access decision
   (entitlement check) that is logged.
5. **Admin is a separate security domain.** Different hostname, SSO + hardware key, IP
   allow-list, no public internet exposure, four-eyes for money and sanctions.
6. **Everything is observable and auditable.** Structured logs, traces, metrics, and an
   append-only hash-chained audit log for privileged actions.
7. **Vendor independence.** Adapters for payments, delivery, verification, email and
   search. No vendor SDK leaks into domain logic.

## 3. Module boundaries

| Module | Owns | Public interface (Go) | Key external deps |
| --- | --- | --- | --- |
| `identity` | users, credentials, sessions, devices, roles, recovery | `identity.Service` | Ory Kratos (or built-in), TOTP/passkeys |
| `catalog` | creator profiles, handles, categories, tags, discovery | `catalog.Service` | Search engine adapter |
| `content` | posts, visibility, tiers mapping, scheduling, stories, vault | `content.Service` | object storage |
| `media` | upload sessions, transcode jobs, renditions, playback tokens, watermarks | `media.Service` | transcoder + CDN adapter |
| `subscriptions` | plans, tiers, subscription lifecycle, promo codes, renewals | `subscriptions.Service` | payment adapter, Temporal |
| `billing` | orders, payments, refunds, chargebacks, invoices, tax records | `billing.Service` | payment adapters |
| `ledger` | accounts, journals, entries, balances, reconciliation | `ledger.Service` | PostgreSQL (TigerBeetle later) |
| `payouts` | balances, payout requests, methods, tax forms, sanctions screening | `payouts.Service` | payout adapter (Paxum/crypto/SEPA) |
| `messaging` | threads, messages, locked media, mass messages, rate limits | `messaging.Service` | — |
| `moderation` | classification results, review queue, decisions, reports, appeals | `moderation.Service` | classifier APIs, NCMEC pipeline |
| `notifications` | push, email, in-app, preferences, digests | `notifications.Service` | FCM adapter, email adapter |
| `analytics` | creator stats, platform stats, funnels, cohorts, risk signals | `analytics.Service` | ClickHouse |

## 4. Request path and trust boundaries

1. Client → Cloudflare (TLS 1.3, WAF, bot score, rate limits, country rules).
2. Cloudflare → edge gateway over mTLS; gateway validates JWT/opaque session, device
   binding and request signature, then routes to the module.
3. Module authorises (RBAC + resource-level entitlement), executes the use case, writes
   to PostgreSQL inside one transaction, appends the outbox event.
4. Worker fleet consumes outbox events: sends notifications, triggers transcodes, calls
   payment vendors, runs reconciliation, updates analytics.

Trust boundaries: client↔edge, edge↔service (mTLS), service↔vendor (outbound allow-list),
admin↔core (separate identity provider), CI↔cloud (OIDC, no long-lived keys).

## 5. Data model highlights

Detailed schema: `docs/database/schema.md` and `backend/migrations/`.

- Identity tables (`users`, `user_credentials`, `sessions`, `devices`, `roles`).
- Creator surface (`creator_profiles`, `creator_tiers`, `creator_categories`).
- Content (`posts`, `post_assets`, `post_tiers`, `stories`, `media_assets`,
  `media_renditions`, `playback_tokens`).
- Money (`plans`, `subscriptions`, `orders`, `payments`, `refunds`,
  `ledger_accounts`, `ledger_journals`, `ledger_entries`, `wallets`, `payout_requests`,
  `tax_profiles`).
- Safety (`moderation_jobs`, `moderation_decisions`, `reports`, `appeals`,
  `audit_log`, `risk_signals`).

Partitioning strategy: time-range partitions for high-volume append-only tables
(`ledger_entries`, `audit_log`, `media_play_events`, `notifications`); hash partitioning
by `creator_id` for creator-scoped content where needed.

## 6. Failure and consistency model

| Concern | Approach |
| --- | --- |
| Payment vendor timeout | Durable workflow retries with exponential backoff; idempotency key per attempt; reconcile daily |
| Double charge | Client idempotency key + ledger uniqueness on `(journal_id, account_id, direction)` |
| Partial subscription activation | Single transaction writes order + payment + entitlement + outbox event |
| Transcode failure | Job retry with backoff; asset marked `failed` after N attempts; creator notified |
| CDN token leaked | Tokens are ≤5 minutes, bound to session+device, and revocable per user |
| Vendor outage | Multi-vendor routing for payments; fallback CDN; degraded-mode feature flags |
| Region loss | Multi-AZ by default; documented DR region with PITR restore drill quarterly |

## 7. Environments

| Env | Purpose | Data |
| --- | --- | --- |
| local | docker-compose: Postgres, Valkey, MinIO, mailpit | synthetic only |
| staging | production-like, real vendors in sandbox mode | synthetic only |
| production | live | real |

No production data is ever copied to staging or local.
