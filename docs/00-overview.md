# 00 — Product Overview

## What we are building

A global subscription platform for adult creators (+18). Creators publish free and
exclusive paid content; subscribers pay for access through subscriptions, membership
tiers, single-item purchases and tips. The platform takes a commission and provides
hosting, delivery, payments, protection, moderation, discovery and analytics.

## Content policy (fixed product decision)

| Surface | Allowed | Prohibited |
| --- | --- | --- |
| Public (unauthenticated / unpaid) | Non-nude imagery, lifestyle, promotional posts | Nudity, sexual suggestion, explicit content |
| Exclusive (age-verified + paid subscribers) | Non-explicit nudity, suggestive content | **Explicit/pornographic sexual content, always** |
| Anywhere | — | Minors (any depiction), non-consensual content, violence, bestiality, incest themes, illegal acts |

This policy is not negotiable and is enforced technically: no payment method, payment
provider, delivery vendor or store presence survives a platform that drifts into explicit
content. The prohibition is therefore an engineering requirement, not a marketing choice.

## Personas

1. **Subscriber (fan)** — discovers creators, subscribes, buys single items, tips,
   messages, manages renewals and privacy.
2. **Creator** — builds a page, sets prices and tiers, uploads and schedules content,
   manages subscribers and messages, watches analytics, requests payouts.
3. **Moderator / Trust & Safety** — reviews queued content, handles reports, applies
   sanctions, documents every decision.
4. **Finance / Risk operator** — monitors transactions, reconciles the ledger, handles
   chargebacks and payout reviews under four-eyes control.
5. **Administrator** — manages users, roles, configuration, feature flags and audit.

## The ten product pillars

1. **Identity & access** — accounts, roles, sessions, devices, 2FA/passkeys, recovery.
2. **Age & identity assurance** — age verification for adult surfaces, KYC for payouts.
3. **Content & media** — upload, transcode, protected delivery, posts, stories, vault.
4. **Monetisation** — subscriptions, tiers, PPV, tips, promos, free trials.
5. **Messaging** — direct messages with paid/locked media and compliance guardrails.
6. **Money** — double-entry ledger, wallet, commission, payouts, refunds, reconciliation.
7. **Protection** — screenshot control, watermarking, signed URLs, session binding,
   concurrency limits, leak detection and takedown.
8. **Moderation & safety** — automated classification, human review, appeals, reports.
9. **Discovery & analytics** — search, categories, recommendations, creator and
   platform analytics.
10. **Platform operations** — admin console, observability, audit, SLOs, DR, handover.

## Non-goals

- We are not a general-purpose social network, a digital-goods marketplace, or a
  dating service. Scope control protects the subscription model.
- We do not build our own card acquiring or become a payment institution. We integrate
  regulated vendors.

## Success criteria (definition of "competitive")

See `docs/roadmap.md` for the full measurable target set. Headline targets:
p95 video start < 1.5 s, crash-free sessions ≥ 99.5 %, upload success ≥ 99 %,
payout latency ≤ 24 h domestic / ≤ 72 h international, moderation decision
< 4 h p50, leak response < 24 h detection / < 72 h takedown, ≥ 4 membership tiers,
API availability 99.9 %.
