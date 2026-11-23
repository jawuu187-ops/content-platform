# Jurisdiction and provider assessment — revision 2

**Date:** 26 November 2026
**Supersedes:** the NO-GO conclusion in `payments-eligibility-review.md` (D-057), which wrongly
assumed the platform would operate as an Egyptian domestic service because the founder lives in
Egypt.
**Status:** research and documentation only. No company was formed, no provider or lawyer was
contacted, no application submitted, no money spent. Payments stay disabled in the product and
every code safeguard from D-058 stays in place.

---

## 1. What revision 1 got wrong

Revision 1 collapsed seven separate legal questions into one. They are not the same question and
they do not have the same answer.

| # | Layer | Intended answer |
| --- | --- | --- |
| 1 | Founder's personal country of residence | **Egypt** (individual, not a jurisdiction of operation) |
| 2 | Jurisdiction of incorporation | **To be chosen** — shortlist in §3 |
| 3 | Country from which the platform is managed | **Remote-first**; servers, staff and contractors outside Egypt |
| 4 | Countries where creators are legally based | **US, UK, EU, Canada, Australia** — creator ToS restricted to these |
| 5 | Countries where subscribers are located | Worldwide **excluding Egypt**, sanctioned countries and any jurisdiction that prohibits the content |
| 6 | Where payment processing and settlement take place | **US / UK / EU** processor entity, fiat settlement to the company's bank account in its own jurisdiction |
| 7 | Where the app is available or restricted | **Geo-blocked in Egypt** at CDN, checkout and app; ToS prohibition |

Only layers 1 and 5 touch Egypt at all, and layer 5 is a blocking decision, not a location.
Revision 1's verdict was therefore too broad. The correct finding is narrower: **the Egyptian
prohibitions do not govern an operator that is incorporated, managed, hosted and settled outside
Egypt, and that does not serve Egyptian users — but they are not automatically irrelevant to an
Egypt-resident founder either.** That residual question is answered in §2 and belongs to counsel.

---

## 2. The Egyptian exposure that remains, stated precisely

Egyptian criminal jurisdiction is territorial (Penal Code art. 1). Two provisions reach acts
committed abroad, and both matter here.

**Art. 3 (active personality, the founder's exposure).** Verified text: *"Every Egyptian who,
while outside the country, commits an act deemed a felony or misdemeanor under this Law shall be
punished in accordance with its provisions **if he returns to the country and the act is
punishable under the law of the country in which it was committed**."*

Source: Penal Code No. 58 of 1937, art. 3 (Arabic and French text):
<https://librejustice.fr/norm/eg/code-penal/3>. Art. 1–4 summarised with the same wording by
ICMEC's Egypt legislation compilation:
<https://www.icmec.org/wp-content/uploads/2018/10/ICMEC-Egypt-National-Legislation.pdf>.
Practice note on dual-national Egyptians and art. 3:
<https://mostafaelroby.com/en/criminal-liability-of-dual-national-egyptians-for-crimes-committed-abroad/>.

Two conditions are cumulative, and the second is the one that matters:

- **Condition A — the act is a felony or misdemeanour under Egyptian law.** Publishing obscene
  material for trade or distribution is (art. 178, as amended by Law 189/2020).
- **Condition B — the act is also punishable in the country where it was committed.** Operating
  a licensed adult creator platform in a state where that business is lawful does not satisfy
  condition B. Where condition B fails, art. 3 does not attach.

**Art. 2 (accomplice acts with a part in Egypt).** Egyptian law applies to a person who, abroad,
is an accomplice in an offence **wholly or partly committed in Egyptian territory**. This is the
provision that stops "incorporate abroad" from being a complete answer: any act of the business
that physically happens in Egypt — a server, an editor uploading content from Cairo, a support
agent answering from Egypt, an Egyptian bank account receiving proceeds, advertising to Egyptian
users — can be characterised as part of the offence being committed in Egypt. Art. 4 adds a
procedural bar: prosecution for an act abroad requires the Public Prosecution and is barred where
a foreign court has acquitted or finally sentenced the person.

### Consequences for the operating design

These are engineering and policy decisions, not legal advice, and each one removes a factual
hook that art. 2 could otherwise use:

1. **No infrastructure in Egypt.** Application servers, database, object storage and CDN nodes
   outside Egypt; no Egyptian hosting provider; no Egyptian domain registrar requirement.
2. **No content handling performed from Egypt.** Uploads, moderation, editing and publishing are
   done by contractors in the company's jurisdiction or by automated pipelines outside Egypt.
3. **No Egyptian users, no Egyptian marketing, no Egyptian payment methods.** Geo-block at the CDN
   and at checkout, refuse Egyptian IPs and Egyptian card issuers (BIN), and exclude Egypt in the
   ToS.
4. **Settlement outside Egypt.** Proceeds are received by the company's bank account in its own
   jurisdiction. Repatriating income into Egypt is a separate question under Central Bank FX rules
   and must be answered before any distribution to the founder.
5. **No crypto anywhere in the money path.** Law 194/2020 art. 206 concerns issuance, trading and
   promotion of crypto assets; keeping fiat-only settlement removes that exposure entirely and
   keeps the option of an Egyptian banking relationship clean.

### The residual question for counsel (not answerable by research)

An Egypt-resident founder who performs management and product work from Egypt, on a business
that is lawful where incorporated, is the hard case. The specific questions a qualified Egyptian
lawyer must answer before launch:

1. Does approving content, setting prices or directing a foreign adult platform from Egypt
   constitute an act "partly committed" in Egypt for art. 2?
2. Does art. 3 attach where the conduct is lawful in the state of incorporation and of operation?
3. What is the treatment of a founder who is a shareholder but not a director, and does that differ
   from an operating director?
4. What are the FX and tax reporting obligations for a resident holding a foreign company and
   receiving distributions?
5. Is there any Egyptian rule that would treat a foreign platform serving Egyptian-diaspora users
   as publishing in Egypt?

Until those are answered, the project can build and test but must not take live payments.

---

## 3. Where a company could be established

Criteria applied: adult content lawful, creator-platform business permitted, serviceable by a
processor that boards this model, credible banking path, and reasonable formation cost. Fees below
are **published government/registry fees**, not quotes; they exclude legal, accounting and
registered-office costs.

| Jurisdiction | Adult platform lawful | Formation cost (published) | Ongoing | Processor coverage | Notes / risks |
| --- | --- | --- | --- | --- | --- |
| **Delaware LLC (US)** | Yes; obscenity law varies by state, and 18 U.S.C. §2257 record-keeping applies to producers | $90 filing + registered agent ~$50–150/yr | **$300/yr** Delaware LLC franchise tax | SegPay (US), CCBill (US) | Best processor fit. US banking for adult merchants is restrictive; §2257 records and possible state-level obscenity exposure need a US attorney. Single-member LLC is pass-through, so the founder's Egyptian tax position matters |
| **Wyoming LLC (US)** | Same as above | $100 filing + ~$25–100/yr agent | ~$60/yr annual report | SegPay, CCBill | Cheaper than Delaware, less precedent with processors; same banking and §2257 issues |
| **UK Ltd** | Yes; adult content lawful, but the Online Safety Act imposes age-assurance and illegal-content duties | £50 incorporation (Companies House) | Confirmation statement £34, accounts filing, registered office | SegPay (UK entity, payouts through UK banking network) | Processor and regulatory regimes are well defined; UK business banking is the obstacle — mainstream banks and most EMIs exclude adult. Also UK VAT registration if UK subscribers are served |
| **Ireland Ltd** | Yes (EU member) | ~€500 via company formation agent; no notary | Annual return + audit exemption thresholds | SegPay **Ireland** entity settles through European banking network | SegPay's own EU licensing story is Irish; an Irish company is the most natural counterparty for it |
| **Netherlands BV** | Yes | Notary required, ~€1,000–2,000 | Dutch accounts filing; CIT 19–25.8% | SegPay EU, Verotel and other EU adult specialists | Strong adult-processing ecosystem; BV needs a notary and a real EU address; Dutch bank/EMI for adult still must be confirmed |
| **Cyprus Ltd** | Yes (EU) | ~€1,000–1,500 incl. nominee-free setup | Audit required for all companies; 12.5% CIT | SegPay EU | Cheap EU company and familiar to processors, but banking for adult content is the weak link |
| **Estonia OÜ (e-Residency)** | Yes (EU) | ~€500 + state fee €265 + e-Residency card €100–300 | Annual report | SegPay EU | e-Residency gives administration, **not** banking; an EMI that accepts adult must be found first |
| Germany (GmbH/UG) | Yes, but **JMStV** imposes strict youth-protection and age-verification duties | Notary €1,000+ | Accounts, trade tax | SegPay EU | Heaviest compliance load of the EU options; not recommended as a first structure |
| UAE / Saudi / Qatar / Kuwait | **No** — criminal exposure | — | — | — | Excluded. Not a candidate at any price |
| Georgia, Armenia, Turkey | Adult content illegal or heavily restricted | — | — | No processor coverage | Excluded |

**Shortlist for a decision:** Ireland Ltd or UK Ltd as first choice (processor counterparty and
licensing alignment), Delaware/Wyoming LLC as the alternative if US processing and a US bank are
easier to obtain. Netherlands BV only if an EU adult-specialist processor is chosen instead of
SegPay.

**Not decided here.** Jurisdiction is a business, tax and legal decision for the owner with
counsel. This document only narrows the field from "anywhere" to three serious candidates.

---

## 4. Providers reassessed against an international structure

Legend: **V** = verified in provider documentation, **C** = provider claim, **?** = unknown,
requires written confirmation. "US/UK/EU company" means the company of layer 2, not an Egyptian
entity.

| | SegPay | CCBill | PaymentWings | CryptoPayr | RazPay | HighRiskTech |
| --- | --- | --- | --- | --- | --- | --- |
| Boards the company in US / UK / EU | **V** US, UK, Europe (FAQ); site states US, UK, EU **and Australia** | **V** US, Canada, EU, UK (from earlier research this session) | **?** no country list beyond a blocked-country page | **?** not addressed | **?** not addressed | **C** "150 countries" |
| Adult creator platforms | **V** — "payment processing built for creators, adult…"; creator-platform industry page | **V** — adult and creator platforms since 1998 | **V** — AUP allows adult, names creator platforms | **V** — dedicated adult page | **V** — adult accepted, reviewed per merchant | **C** — marketing only |
| Marketplace / commission model | **V** — creator platforms are a named vertical; payout mechanics per contract **?** | **V** — platform model long established; creator payout product **?** (needs confirmation) | **?** — payouts API, no split primitive | **V** — sub-accounts, commission, mass payouts | **?** | **?** |
| Cards | **V** Visa/MC/Maestro/JCB/Discover, PayPal, SEPA | **V** | **V** | **NO** | **V** via Solana NFT voucher | **C** |
| Recurring subscription engine | **V** core product (one-time, recurring, one-click, retention) | **V** automated engine | **? / effectively NO** (invoice-style re-billing) | **?** | **?** | **?** |
| Fiat settlement to company bank | **V** — weekly payout cycle, $125 minimum, named acquiring banks (US, Worldline/Belgium, Credorax/Malta, PXP/UK, Valitor/Iceland, Clearhaus/Denmark) | **V** fiat settlement | **NO** — crypto only | **NO** — AML policy excludes fiat | **NO** — USDC/Polygon | **?** |
| Settlement currency / region | 15 currencies; US entity pays via US banks, Ireland entity via European network | USD/EUR/GBP **?** | crypto | crypto | USDC | **?** |
| Beneficial owner nationality/residence acceptance | **?** — KYC file requires passports of all >20% owners but no published nationality rule; **the Egypt-resident UBO is the single most important question to ask before applying** | **?** same question | moot | moot | moot | **?** |
| Creator locations accepted | **?** — creators are the merchant's counterparties, not the merchant; needs confirmation that creator payouts to US/UK/EU individuals are supported | **?** | moot | moot | moot | **?** |
| Costs | Card-brand registration Visa **$950/yr**, Mastercard **$500/yr** for adult/dating/nutraceutical; transaction rate quoted per merchant; rolling reserve per contract **?** | Visa $950/yr, MC $500–1,000; rates **?** | 6%→2% + undisclosed method fees | 1% + $0.10 | 5.2%–13% | quote only |
| Public API | **V** merchant documentation site | **V** | **V** REST + signed webhooks + sandbox | **V** | **C** | **NO** |
| Verdict for this business | **Primary candidate** | **Primary candidate** | Rejected: crypto settlement, no renewal engine, no KYB | Rejected: crypto only, no KYB | Rejected: crypto settlement, NFT-voucher card flow | Not evaluable |

### Why the verdict changes
Revision 1 rejected SegPay and CCBill because an Egyptian company cannot board. That was a
finding about the *company*, not about the *model*. With the company in Ireland, the UK or the
US, both are in scope on the published criteria: adult creator platforms are a named vertical, a
real recurring billing engine exists, settlement is fiat to the merchant's own bank, and the card
scheme registrations are published. What remains unknown is company-specific underwriting — the
beneficial owner's Egyptian residence, the marketplace/commission structure, and creator payouts —
and those are exactly what a written pre-application answer is for.

The three crypto-first leads stay rejected for an unrelated reason: crypto settlement is
unusable **for the founder** while he is resident in Egypt (Law 194/2020 art. 206), and none of
them runs a genuine subscription engine.

---

## 5. Requirements, costs and restrictions by layer

### 5.1 Company and compliance (annual, approximate)
- Formation: $90–$1,500 once, depending on jurisdiction (§3).
- Registered office / agent: $50–$300/yr.
- Statutory accounts and filings: $300–$1,500/yr (audit required in Cyprus; not in Ireland below
  thresholds; US LLCs file a franchise tax/annual report only).
- Processor: card-brand registration **$1,450/yr** (Visa $950 + Mastercard $500) for adult
  merchants at SegPay; transaction rate and rolling reserve on quote — budget a **5–10% rolling
  reserve held 6 months** until a contract says otherwise.
- Age verification vendor: unpriced (AV-1..AV-4 still open).
- Records: 18 U.S.C. §2257-style custodian-of-records obligations if any US nexus or US creators
  are involved — this is a real operating cost, not paperwork.
- VAT: EU OSS registration for EU subscribers, UK VAT if UK subscribers are served, and US state
  sales tax where digital goods are taxable. None of this exists yet.

### 5.2 Regulatory duties tied to subscribers (the part that scales with geography)
- **UK Online Safety Act**: age assurance for adult content, illegal-content risk assessment,
  transparency duties. This is why the existing age-gate design is load-bearing, not decorative.
- **EU Digital Services Act**: trader traceability (KYBC), notice-and-action, transparency
  reporting — applies as soon as EU subscribers exist.
- **US state age-verification statutes** (Louisiana, Texas and others): card-and-ID requirements
  for sites whose content is a substantial portion of the service.
- **Card scheme rules**: adult content merchants must be registered with Visa/Mastercard and
  follow their age-verification and descriptor rules.

### 5.3 Availability and blocking
- Egypt: blocked at CDN and checkout, refused by BIN, excluded in ToS.
- Sanctioned/embargoed countries: refused (processor requirement, not optional).
- Countries where the specific content category is criminal: refused at the category level, not
  only the country level.
- Geo-blocking must be enforced server-side in the access decision, not by hiding a button.

### 5.4 Money transmission
If the platform collects subscriber money and then pays creators, that is money transmission in
several jurisdictions and would require licensing. The structural answer is to let the processor
be merchant of record (it charges subscribers and pays creators), or to confirm in writing that
the chosen processor's creator-payout product covers the marketplace model. This is a **design
gate before integration**, not a detail.

---

## 6. Revised verdict

**Conditional GO, with gates. The model is not impossible and the project should not be closed.**

1. ~~NO-GO for an Egypt-based operator~~ → **An Egyptian-incorporated and Egypt-operated service
   is not viable.** Correct, and unchanged.
2. **An internationally incorporated, remotely managed, non-Egyptian-facing platform is viable in
   principle** — Ireland, UK or US company; SegPay or CCBill; fiat settlement to the company's own
   bank; Egypt blocked.
3. **Before any integration**, four things must be settled, in this order:
   - **a. Jurisdiction decision** (owner + counsel): Ireland Ltd, UK Ltd, or Delaware/Wyoming LLC.
   - **b. Egyptian counsel opinion** on arts. 2 and 3 and on FX/tax treatment of the resident
     founder (§2). Nothing launches before this.
   - **c. Written provider answer** from SegPay or CCBill on: Egyptian-resident UBO acceptance,
     creator-platform marketplace with commission, creator payouts to US/UK/EU individuals, fiat
     settlement to the chosen jurisdiction's bank, and the complete fee/reserve schedule. This is
     an information request, and it discloses the project, so it needs the owner's explicit
     approval first.
   - **d. Banking**: a bank or EMI in the chosen jurisdiction that will hold adult-platform
     proceeds. Unresolved for every option in §3 and capable of killing a jurisdiction choice.

**Until a, b, c and d are answered and approved: payments stay disabled, `/v1/meta` keeps
reporting `payments.available=false`, the fake provider stays confined to `APP_ENV=local`, and no
screen shows a balance, earning or receipt.**

---

## 7. Open items this revision creates

| # | Item | Owner | Blocks |
| --- | --- | --- | --- |
| JUR-1 | Choose incorporation jurisdiction with counsel | Owner | everything in §4 |
| JUR-2 | Egyptian counsel opinion: Penal Code arts. 2 and 3 as applied to a resident founder of a foreign, Egypt-blocked platform | Owner | launch, not build |
| JUR-3 | Egyptian FX/tax treatment of distributions to a resident shareholder | Owner + accountant | launch |
| JUR-4 | Written pre-application answer from SegPay and/or CCBill (UBO residence, marketplace, creator payouts, settlement, fees) | Owner approval needed before any contact | provider integration |
| JUR-5 | Adult-tolerant bank/EMI in the chosen jurisdiction | Owner | settlement |
| JUR-6 | Confirm CCBill country list and creator-payout product from its own documentation (search allowance exhausted this session) | Assistant, next session | §4 table |
| JUR-7 | Confirm Verotel's merchant-country and fee terms as an EU alternative | Assistant, next session | §3 Netherlands option |

## Sources used in this revision

- Egyptian Penal Code arts. 1–4 (territoriality, accomplice acts, active personality with dual
  criminality, procedural bar): <https://librejustice.fr/norm/eg/code-penal/3>,
  <https://www.icmec.org/wp-content/uploads/2018/10/ICMEC-Egypt-National-Legislation.pdf>,
  <https://mostafaelroby.com/en/criminal-liability-of-dual-national-egyptians-for-crimes-committed-abroad/>
- SegPay merchant regions, KYC file, acquiring banks, currencies, card registration fees, FCA
  reference: <https://cs.segpay.com/faq/>; regions including Australia, creator/adult verticals:
  <https://segpay.com/>; EU/UK licensing and Ireland entity:
  <https://segpay.com/newsroom/segpay-ready-to-process-in-european-union-and-united-kingdom/>
- Penal Code art. 178 and Law 194/2020 art. 206, CBE warning: as cited in
  `payments-eligibility-review.md` §1.
- PaymentWings, CryptoPayr, RazPay, HighRiskTech: as cited in `payments-eligibility-review.md` §2.
