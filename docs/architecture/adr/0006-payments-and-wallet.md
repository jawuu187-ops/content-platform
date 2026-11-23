# ADR-0006 — Payment architecture: multi-vendor cards + prepaid wallet balances + crypto rails

**Status:** Accepted — 2026-11-23

## Context
Mainstream processors (Stripe, PayPal, Square, Shopify Payments, Revolut Business) prohibit
this business model outright. Adult-friendly acquiring carries high rates, rolling reserves
and onboarding friction. Crypto rails remove card-network dependency but cannot natively
express "recurring subscription" without user re-approval, and creator payouts must reach
many countries.

## Decision
Three cooperating layers:

1. **Card layer** — integrate two or more adult-friendly providers behind one
   `PaymentProvider` interface, with smart routing, per-provider health, and a fallback
   provider for every market. Hosted fields/redirect only (SAQ-A).
2. **Prepaid wallet layer (the key decision)** — a user can top up an internal balance with
   any supported rail (card, crypto, local transfer). Renewals, PPV and tips debit that
   balance through the ledger. This makes recurring billing possible in crypto-only
   markets, removes repeated small-card transactions (and their fees), and reduces
   chargeback exposure because the fan prepaid.
3. **Payout layer** — creator withdrawals through multiple rails (adult-native wallet,
   SEPA/ACH from the operating entity, crypto mass payout). Rail selection is per-creator
   based on country, cost and speed.

## Why the wallet matters commercially
At new-merchant adult rates (roughly 11–15 % plus reserves), a flat 20 % platform
commission can leave the platform with single-digit effective margin after processing,
chargebacks, payout fees and FX. Prepaid balances, tiered commission and negotiable volume
pricing are the levers that make the model viable. This is recorded in
`docs/vendors/economics.md`.

## Consequences
- We must never be a regulated money transmitter: balances are only usable inside the
  platform, are non-transferable, and are not refundable to a bank account. This is a
  product and legal constraint enforced by design.
- Stored-value balances bring their own regulatory questions in some jurisdictions; the
  operating entity's counsel must clear the model before launch.
