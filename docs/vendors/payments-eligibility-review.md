# Payment provider eligibility review — Egypt-based operator

**Date:** 26 November 2026
**Scope:** eligibility review only. No account was opened, no application submitted, no
provider contacted, no plan purchased. Every provider below was assessed from its own public
documentation.

> **Revision 2 — read `jurisdiction-and-provider-assessment.md` before acting on this file.**
> This revision wrongly treated the platform as an Egyptian domestic service because the founder
> lives in Egypt, and concluded NO-GO for the whole business. That conclusion holds only for a
> company incorporated and operated in Egypt. For an internationally incorporated, Egypt-blocked
> platform the correct answer is *conditional GO with gates*: SegPay or CCBill become genuine
> candidates once the company sits in Ireland, the UK or the US. The legal detail is unchanged and
> still correct; the scope of the verdict was too wide.

**Verdict as written (revision 1): NO-GO for an Egypt-incorporated, Egypt-operated service.** No
provider on the list can lawfully serve that structure. The blocker is not the providers' appetite
for adult content — three of the four explicitly accept it. The blocker is Egyptian law and the
settlement route.

---

## 1. The two facts that decide this

### 1.1 A paid adult-content platform operated from Egypt is a criminal offence

Egyptian Penal Code article 178 (as amended by Law 189/2020) punishes anyone who publishes,
manufactures or possesses obscene material **for trade, distribution, rental, posting or
display** with imprisonment of up to two years and a fine of EGP 5,000–10,000, or either
penalty.

- Text of the article, current version: <https://fahmqanon.com/laws/58-1937/178>
- Egyptian legal press summary of the same article and its 2020 amendment:
  <https://www.elbalad.news/7137180>
- English translation of the Penal Code (article 178): <https://www.clr.africanchildforum.org/Legislation%20Per%20Country/Egypt/egypt_penal_1937_en.pdf>

This is not a payments question. A subscription platform that sells access to nude or
suggestive imagery is squarely "obscene material … for trade or distribution" under that
text. Registering the company abroad does not remove the exposure of a founder who resides
in and operates from Egypt; that is a question for Egyptian counsel, not for a processor.

### 1.2 Crypto settlement is not a lawful route for this operator either

The Central Bank and Banking System Law (Law 194/2020) article 206, and the CBE's own
warning, prohibit issuing, trading, promoting or operating platforms for crypto assets without
CBE approval, with penalties of EGP 1–10 million and/or imprisonment.

- CBE warning statement (13 Sep 2022), naming Law 194/2020 and the penalties:
  <https://www.cbe.org.eg/en/news-publications/news/2022/09/12/warning-statement>
- Baker McKenzie regulatory guide: no crypto licensing regime exists; FRA opposes any crypto
  activity: <https://resourcehub.bakermckenzie.com/en/resources/global-financial-services-regulatory-guide/europe-middle-east-and-africa/egypt/topics/what-types-of-activities-require-a-license-in-your-jurisdiction>

Three of the four leads settle **only** in crypto. That alone disqualifies them for an
Egyptian operator, independent of everything else.

---

## 2. Comparison

Legend: **V** = verified in the provider's own documentation; **C** = provider claim, not
independently confirmable; **?** = not documented, unresolved.

| | PaymentWings | CryptoPayr | RazPay | HighRiskTech | SegPay (comparator) | CCBill (comparator) |
|---|---|---|---|---|---|---|
| Entity / base | Belize company, 195A Swordfish Ave, Vista Del Mar | CryptoPAYR Limited, Cayman; FINTRAC MSB M/2264-958/5 | RazPay Inc., English/Japanese support | London address (8 New St Square) | US (Florida), FCA FRN 584599 | US |
| Accepts adult **content platforms** | **V** — AUP allows "adult entertainment"; industry page names "creator platforms, memberships" | **V** — dedicated adult page, "consent content, you verify age" | **V** — adult accepted, individually reviewed, approval not guaranteed | **C** — marketing lists "content platforms, content creation" | **V** — adult + creator platform billing | **V** — adult, creator platforms since 1998 |
| Egypt-based merchant | **?** — Egypt is absent from its blocked-country list, which is the only published signal | **?** — not addressed; settlement is crypto, so moot | **?** — no country list published | **C** — "150 countries", no list | **V — NO**: "merchants located in the United States, United Kingdom, and Europe" | **V — NO**: boards US, Canada, EU, UK merchants |
| Marketplace / platform with commission | **?** — payouts API for sending balance out; no split-payment or commission primitive documented | **V** — sub-accounts, platform commission on each sale, mass payouts | **?** | **?** | **C** — supports "creator platforms"; split-payout mechanics not published | **C** — "social media platforms"; split mechanics not published |
| Card payments | **V** — Visa/Mastercard, Apple/Google Pay | **NO** — crypto only | **V** — Visa/Mastercard, via an NFT voucher | **C** — "credit card processing" | **V** — Visa/MC/Maestro/JCB/Discover/PayPal/SEPA | **V** — cards, recurring engine |
| Recurring subscriptions | **? / effectively NO** — no subscription object or stored-credential renewal in the API; own docs describe creating a payment per billing cycle (invoice-style) | **?** — no renewal engine; merchant activates access on confirmation | **?** — "continuity" accepted, no subscription API published | **?** | **V** — recurring billing is core | **V** — automated subscription engine |
| Settlement | **Crypto only** — "get paid in crypto", balance held in crypto, crypto payout endpoints | **Crypto only** — AML policy: "does not offer direct fiat conversion or fiat settlement" | **USDC on Polygon**, month-end close paid end of following month | **?** — "1 day payouts", method unpublished | **Fiat** to merchant bank; named acquirers; weekly payout day, $125 minimum | **Fiat**; no payout schedule published |
| Setup / monthly | **V** — none, no monthly minimum | **V** — none | **V** — $0 / $0 | **?** | **V** — none published; Visa registration $950/yr, MC $500 (high-risk) | **V** — Visa $950/yr, MC $500–1,000 |
| Transaction rate | **C** — from 6% falling to 2% with volume, plus undisclosed per-method fees | **V** — 1% + $0.10; 0.5% + $0.10 above $5k/mo; 0.1% + $0.10 above $15k/mo | **V** — from 5.2%; high-risk 10–13% | **?** — "request a quote" | **?** — no published rates | **?** — no published rates |
| Reserve / chargeback | **C** — no rolling reserve; claims payments "cannot be reversed" (not a card-network standard) | n/a crypto (irreversible) | **?** — not published | **?** | **C** — chargeback tools; terms quoted per merchant | **C** — chargeback tools; terms quoted per merchant |
| Refund / chargeback / FX / payout fees | **?** | **?** — pricing page lists white-label, conversion and refund fees but the figures did not render | **?** | **?** | **?** | **?** |
| KYB / KYC | **C** — "no KYC to start" (red flag: incompatible with our own AML/KYB duty) | **V** — explicit: no KYC or KYB at all | **?** — merchant review includes age verification, refund and cancellation controls | **?** | **V** — full KYC file: corporate docs, ID of >20% owners, bank statement, tax ID | **V** — application + underwriting |
| Onboarding documents | account in ~2 minutes, then unclear | email only | review-based; docs list unpublished | form + email | corporate docs, passports, bank statement, tax ID/VAT, domain ownership | application, underwriting |
| Public API / docs | **V** — REST, signed webhooks, sandbox | **V** — API, plugins, sandboxes | **C** — API integration offered, no public docs | **NO** — sales-led only | **V** — merchant documentation site | **V** |
| Sources | <https://paymentwings.com/industries/adult> · <https://paymentwings.com/legal/acceptable-use-policy> · <https://paymentwings.com/legal/geographic-restrictions> · <https://paymentwings.com/docs/> | <https://cryptopayr.com/industries/adult> · <https://cryptopayr.com/legal/aml> · <https://cryptopayr.com/pricing> | <https://www.raz-pay.com/adult-payment-processing/> · <https://www.raz-pay.com/terms.html> | <https://highrisktech.com/adult-merchant-account/> · <https://highrisktech.com/apply-now/> | <https://cs.segpay.com/faq/> · <https://segpay.com/> | <https://partners.ccbill.com/English/> |

---

## 3. Provider-by-provider reading

### PaymentWings
Closest of the four on paper for our model: adult content is explicitly allowed, creator
platforms are named, cards are accepted, there is a documented REST API with signed webhooks
and a sandbox, and the fee schedule is stated (6% → 2%, no setup, no monthly minimum, no
rolling reserve).

It still fails three requirements:

1. **Settlement is crypto.** Their own pages say "get paid in crypto" and the API exposes
   crypto payouts only. That is not a lawful fiat route for an Egyptian operator (§1.2).
2. **No real recurring engine.** The API has payments, links, products, balance and payouts;
   there is no subscription object, no stored credential, no retry/dunning model. Their own
   integration advice is to "create a payment for each billing cycle" — invoice-style
   re-billing, which is not a subscription renewal and pushes dunning onto us.
3. **No KYC at signup, and "no chargebacks".** A provider that both skips KYB and guarantees
   irreversibility is not operating a conventional card acquiring model. We cannot build
   compliance on top of a counterparty whose own onboarding is incompatible with ours, and
   the blocked-country list is the only published geography signal — absence of Egypt from
   that list is not an approval.

### CryptoPayr
The most complete *platform* feature set (sub-accounts, commission on sales, mass payouts,
110+ coins), the cheapest headline rate (1% + $0.10), and explicit adult acceptance.

Disqualified outright for us: their AML policy states plainly that CryptoPayr "does not offer
direct fiat conversion or fiat settlement". A USD balance that can only leave to a crypto
wallet is not a lawful settlement route in Egypt. Their no-KYC/no-KYB stance also conflicts
with our own AML obligation, and a card-less checkout removes the payment method most
subscribers actually want.

### RazPay
Accepts adult merchants, reviews age-verification and refund controls, has no setup or
monthly fee, and quotes 5.2%–13% for high-risk.

Disqualified on two independent grounds:

1. **Settlement is USDC on Polygon**, paid a month after month-end. Crypto again.
2. **The card flow is an NFT voucher.** The customer buys a same-value NFT on Solana which is
   "exchanged in real time" for the content. That structure needs its own legal and accounting
   opinion (consumer law, VAT, what the customer actually bought, how a refund works) before it
   can be sold to subscribers, and their own terms describe it as an "escrow transaction
   service", not a merchant account. No recurring or marketplace capabilities are documented.

### HighRiskTech
Every substantive question is unanswered in public: no country list, no settlement method, no
pricing, no API documentation, no statement on recurring billing or platform commissions. It is
a sales-led broker page ("send a request and get a fee rate"). It cannot be evaluated, and no
credible decision can rest on "24 hours approval" and "150 countries" marketing figures. Treat
it as a lead for a future, non-Egyptian entity only.

### Comparators worth naming
SegPay and CCBill are the two providers that actually match our model — adult content, creator
platforms, subscription billing with a real renewal engine, fiat settlement to a merchant bank,
published registrations. Both publish that they board merchants only in the **US, UK, EU**
(CCBill adds Canada). They are therefore not options for an Egyptian entity, but they define
what "good" looks like and are the natural first calls if the operating company is ever
established in one of those jurisdictions.

---

## 4. Recommendation

**NO-GO for an Egypt-incorporated, Egypt-operated service. Keep payments unavailable.** No
provider qualifies for that structure, and no integration work should start. (Revision 2 narrows
this: with the company in Ireland, the UK or the US and Egypt blocked, SegPay and CCBill are
candidates — see `jurisdiction-and-provider-assessment.md`.)

Choosing CryptoPayr or RazPay because onboarding is free and fast would mean settling revenue
through a channel that Egyptian law prohibits (Law 194/2020 art. 206) while running a business
that Egyptian law criminalises (Penal Code art. 178). Choosing PaymentWings would mean the same
crypto settlement problem plus inventing our own renewal engine on top of a provider that does
not do KYB.

### What would have to change first

1. **A lawful operating jurisdiction.** A company with its own directors and bank account in a
   country where adult content is lawful and a provider boards that country (US, UK, EU,
   Canada). This is a legal and business decision, not an engineering one.
2. **A written offer, not a web page.** Provider confirmation of: marketplace/creator-platform
   model with commission splits, a subscription engine with retries and dunning, fiat
   settlement to the entity's own bank account, the complete fee schedule (setup, monthly,
   transaction, refund, chargeback, reserve percentage and release period, payout fee, FX,
   minimum volume), and acceptance of the beneficial owner's nationality and residence.
3. **Counsel on personal exposure** if the founder remains resident in Egypt, before any
   launch marketing.
4. **Our own controls ready before boarding**: age verification wired to a real vendor,
   record-keeping equivalent to 18 U.S.C. §2257, DMCA process, moderation policy, refund and
   cancellation policy — several of which are already in the plan.

None of these can be satisfied by code, and none should be assumed.

---

## 5. What changed in the code because of this review

The product rule "no simulated financial activity in the real application" is now enforced,
not just documented:

| Control | Where |
| --- | --- |
| `PAY_PROVIDER=fake` and `ENABLE_FAKE_PAYMENTS` are accepted **only** with `APP_ENV=local`; staging and production refuse to start | `backend/internal/platform/config/config.go`, tests `TestLoadRejectsFakeProviderInProduction`, `TestLoadRejectsFakePaymentsInStaging` |
| Unknown provider names are rejected, so a half-configured gateway cannot silently become "unavailable but looks configured" | same |
| `/v1/meta` publishes `payments.available` and `payments.provider` (`unconfigured` today), so a client can refuse to render checkout | `backend/internal/httpapi/server.go`, `TestMetaPublishesPolicy` |
| Creating a payment intent with no provider answers 503 and writes no row | `payments.Service.CreateIntent` (fail-closed), `internal/httpapi/payments.go` |
| The Android checkout sheet takes no money, shows no price and collects no card data; it states that payments are unavailable | `ui/screens/Screens.kt` |
| Studio shows no balance, earnings or transaction history; every figure renders "—" with a "not connected" mark | `ui/screens/Screens.kt` (`Data` gate), `ui/PlatformApp.kt` |

The fake provider remains available to automated tests and local development only. It is never
reachable from a production build, a production configuration or a user-facing flow, and a
simulated payment cannot create an entitlement, a ledger entry or a creator balance outside a
test database.
