# Age verification providers — evaluation and integration contract

Status: **evaluation only. No vendor has been contacted, contracted or paid.**
Decisions here are owner-approval items; the platform code is deliberately
provider-neutral until one is signed.

Last updated: 2026-11-24

## 1. What the platform needs

| Requirement | Why | Where it is enforced |
| --- | --- | --- |
| Answer "is this account 18+?" | Legal exposure and payment-processor terms | `creator` access decision, before any entitlement |
| Result bound to one account | A result for someone else must be unusable | `verify.checks.user_id` + provider reference binding |
| Expiry and re-verification | Assurances go stale, and some providers issue time-limited results | `verify.checks.expires_at`, `ExpireDue` |
| Revocation | Provider retractions, chargeback/fraud findings, support escalations | `verify.checks.revoked_at`, admin endpoint |
| Failed attempts and rate limiting | Brute-force and abuse | `verify.attempts`, start limit per 24 h |
| No identity evidence stored | Data minimisation: a breach must not leak documents or biometrics | schema has no column for documents, selfies, face templates or date of birth |
| Provider swap without redesign | Vendor terms, price and coverage change | `agegate.Provider` seam: `Start`, `ParseCallback`, `Capabilities` |

What we keep: a provider reference, an assurance label, timestamps, and the
outcome. What we never keep: document images, selfie frames, biometric
templates, extracted dates of birth, or the raw provider payload (it is parsed
and discarded).

## 2. Candidate methods

| Method | Typical assurance | Privacy profile | Coverage | Notes |
| --- | --- | --- | --- | --- |
| Document + liveness (hosted by a specialist vendor) | High; the vendor attests document authenticity and a live person | Vendor keeps evidence; we receive a result | Widest, but the vendor must cover the jurisdictions you sell to | Highest conversion friction and highest cost; the standard for regulated markets |
| Age estimation from a selfie (no document) | Medium; statistical estimate with a confidence band | Vendor processes the image; some vendors offer to keep nothing | Strong in the EU/UK, patchier elsewhere | Cheapest and fastest; some regulators accept it only for lower-risk content |
| Bank / credit-card or open-banking check | Medium-high; adult in the account holder's name | The bank holds the evidence; we get a pass/fail | Limited to countries with the required rails | Cheap, but excludes people without the right bank or card |
| Digital identity wallet (EU DI Wallet, state ID apps) | High | User consents to a specific attribute ("over 18") | Growing in the EU | Best privacy story (attribute disclosure, not identity disclosure); not yet global |
| On-device estimation with a vendor attestation | Medium | No image leaves the device in some designs | Device-dependent | Attractive for privacy; assurance level must be confirmed in writing |
| Manual review of a document by our staff | High | **We** would hold evidence | Wherever we operate | Rejected for launch: creates exactly the data-retention liability we are avoiding |

## 3. Vendor shortlist (to be verified before any contract)

These are the vendors worth a technical conversation. Pricing and coverage below
are **not quoted commitments** and must be confirmed in writing; no numbers are
recorded here as fact.

| Vendor | Hosted flow | Callback model | Document + liveness | Age estimation | Notes to verify |
| --- | --- | --- | --- | --- | --- |
| Veriff | Yes | Webhook with signature and retries | Yes | Yes | EU-heavy coverage; ask for the exact assurance vocabulary and retention defaults |
| Sumsub | Yes, highly configurable | Webhook | Yes | Yes | Broad jurisdiction list; ask what the minimum retained payload is |
| Onfido (Entrust) | Yes | Webhook | Yes | Limited | Enterprise contracting; ask about adult-content acceptable use |
| Persona | Yes | Webhook | Yes | Yes | Flexible flows; ask about adult-content acceptable use explicitly |
| Yoti | Yes | Callback | Yes | Yes (age estimation is a first-class product) | Strong on attribute-only disclosure; confirm coverage per country |
| Stripe Identity | Yes | Webhook | Yes | No | Convenient if Stripe already processes payments, but adult content is a restricted business — verify before designing around it |

Screening questions for every vendor, in writing:

1. Do you accept adult-content platforms as customers, in which countries, and
   under what terms?
2. What exactly do you retain, for how long, and can retention be set to zero for
   evidence after the decision?
3. Which assurance levels do you expose, and what do they mean in your contract?
4. Do you support attribute-only results (for example "over 18: true") without
   returning identity attributes?
5. What is the per-check price at our volume, and what happens to a re-check after
   revocation?
6. What are your webhook signing, retry and replay-protection semantics?
7. Which jurisdictions and document types are supported, and what is the fallback
   when a document is unsupported?
8. Do you offer a data-processing agreement that names us as controller and you as
   processor, with sub-processor disclosure?

## 4. Integration contract already implemented

The receiver is built and tested; only the hosted-session call is vendor-specific.

- `Provider.Start` returns a hosted URL and a reference. The generic webhook
  provider cannot start a session, which is why `POST /v1/verify/age/start`
  answers **503 `verification_not_integrated`** today. That is deliberate: the
  platform does not pretend a verification flow exists.
- `Provider.ParseCallback` verifies `X-Age-Signature: t=<unix>,v1=<hex>` — an
  HMAC-SHA256 over `<t>.<raw body>` — inside a 5-minute window, then normalises
  the payload. A vendor adapter replaces the parsing, not the rules.
- `verify.callbacks` makes event application idempotent; a replayed event is
  acknowledged and ignored.
- A callback for an account that does not own the reference is rejected, so a
  leaked reference cannot move a verification between accounts.

## 5. Open owner decisions

| # | Decision | Effect if deferred |
| --- | --- | --- |
| AV-1 | Choose the primary method (document + liveness vs age estimation) | 18+ content stays locked for everyone; the gate is live and denies |
| AV-2 | Choose the vendor and region of processing | `verification_not_integrated` stays the honest answer at `start` |
| AV-3 | Approve the retention window and DPA terms | No production credential can be issued |
| AV-4 | Decide whether selfie-only estimation is acceptable for any market | Cannot launch in markets where documents are the only accepted proof |

No spend is authorised by this document.
