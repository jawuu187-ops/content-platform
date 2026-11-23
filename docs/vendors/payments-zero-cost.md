# Payment gateways — zero-cost eligibility for Egypt (research record)

Date of research: 2026-11-25. **No account was created, no terms accepted, no
document submitted, no transaction attempted.** This file records why no real
gateway was integrated and what would have to change for one to qualify.

## The requirement

- Business model: platform selling subscriptions, pay-per-view unlocks and tips
  for adult creators; platform is the merchant of record for the buyer.
- Operating country: Egypt (owner), buyers worldwide.
- Cost: no setup fee, no annual fee, no monthly plan, no paid API tier, no paid
  trial that converts. Transaction fees are acceptable but must be disclosed —
  they are **not** zero and are not described as such here.
- We may not register a merchant account, accept contracts, or submit legal or
  business documents.

## What was checked

| Provider | Adult content eligible | Egypt merchant eligible | Zero-cost onboarding | Verdict |
| --- | --- | --- | --- | --- |
| Stripe | No — adult content is on the published prohibited list | No direct Egypt merchant accounts for this model | Free to integrate | Not eligible: prohibited business |
| PayPal | No — adult content prohibited | Egypt payouts restricted | Free to integrate | Not eligible: prohibited business |
| Adyen | Restricted at best; adult requires explicit approval with documentation | Egypt requires local entity and KYB | Free to integrate | Not eligible: approval + entity + documents |
| CCBill | Yes (adult since 1998) | No — signup limited to 44 countries in North America and Europe | **No** — Visa $950 + Mastercard $1,000 annual card-brand registration, ~$1,950/year | Not eligible: annual fee and contract |
| Segpay / Verotel (high-risk) | Yes | Contract-dependent | **No** — application fee and rolling reserve are contract terms, not public | Not eligible: fees, reserve, contract |
| Paymob (Egypt) | Not listed as permitted; conservative on high-risk verticals | Yes, CBE-licensed payments facilitator | Integration is free, onboarding is not | Not eligible: needs commercial registry, tax card, KYB; adult not permitted |
| Fawry / Geidea (Egypt) | Not permitted for adult | Yes | Integration free, onboarding requires full company paperwork | Not eligible: same as Paymob |
| MaxPay / PayMix / ECommPay (MENA high-risk) | Yes, case by case | Case by case | **No** — application and reserve terms are contractual | Not eligible: contract, reserve, no public zero-cost path |

Sources consulted:
- Adyen, *Prohibited and Restricted products and services* (updated 2025-11-13): adult content is restricted/prohibited without explicit approval — https://www.adyen.com/legal/list-restricted-prohibited
- PaymentPop, *The Gauntlet of Adult Entertainment Services Payments*: states Stripe's published policy lists adult content among prohibited businesses, and that adult merchant accounts carry setup plus annual card-brand registration — https://paymentpop.com/payments-guidance/credit-card-processing-adult-entertainment-services/
- CCBill merchant review (last checked 2026-10-01): $1,950/year Visa+Mastercard registration, processing rate quoted privately, 44 accepted countries — https://owlwebmasters.com/en/services/ccbill/
- Kalastor Net, *How high-risk payment processing works in the MENA region* (2026): local Egyptian acquirers (Geidea, Fawry, Paymob) are "conservative on high-risk verticals"; MENA high-risk processors operate on contract terms with reserves — https://www.kalastor.net/blog/high-risk-payments-mena/
- Paymob FAQ and site: onboarding requires company documents, CBE-licensed facilitator — https://www.paymob.com/support/faqs , https://paymob.eg/

## Conclusion

No gateway satisfies all three conditions at once (adult-eligible + Egypt +
genuinely zero-cost onboarding). High-risk adult processors are the only ones
that accept the business model, and every one of them charges an annual
card-brand registration, an application fee, or a rolling reserve under contract.
Egyptian domestic acquirers that would accept an Egyptian merchant do not permit
adult content. Mainstream gateways prohibit it outright.

Therefore the milestone ships:

1. a provider interface that a real gateway can implement later without touching
   the ledger, the webhook handler or the entitlement logic;
2. a **fake provider** that exists only to prove the flow in development and CI —
   it moves no money and the API refuses to boot with it in production;
3. an `Unavailable` provider that fails closed when nothing is configured.

## What would make a gateway eligible

- A written fee schedule with no setup or annual fee (transaction-only pricing),
  and adult content explicitly permitted.
- Acceptance of an Egyptian entity, or a partner-of-record arrangement that does
  not require us to sign or submit documents without the owner.
- A sandbox that can be used before any contract exists.

Until then: subscriptions stay `202 payment_required`, pay-per-view stays
locked, and the ledger is exercised only by the fake provider.

## Owner decisions this leaves open

- PAY-1: is a high-risk processor's annual card-brand registration (~$1,950/yr
  per the CCBill review) acceptable once the platform is live?
- PAY-2: which entity and country will hold the merchant account?
- PAY-3: commission schedule (currently a 20% placeholder, `PAY_COMMISSION_BPS`).
- PAY-4: crypto rails — Egypt restricts them; a licensed processor would be
  required.
