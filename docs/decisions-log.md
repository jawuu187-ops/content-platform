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
| D-021 | 2026-11-24 | Password hashing uses PBKDF2-HMAC-SHA256 (600,000 iterations) from the Go standard library, not Argon2id from `golang.org/x/crypto` | Accepted | Keeps the service dependency-free and the build hermetic; parameters are stored per user (`algo$iterations$salt$key`) so upgrading to Argon2id is a rehash-on-login, not a migration. Verified against published PBKDF2-SHA256 vectors in `password_test.go` |
| D-022 | 2026-11-24 | Session tokens are opaque 256-bit random values; only SHA-256 hashes are stored, and rotation revokes the previous row while linking it to the replacement | Accepted | A database leak yields no usable session. Replaying a rotated refresh token revokes the whole family (reuse detection), so a stolen token cannot be used silently. Access tokens are 15 min, refresh 30 days |
| D-023 | 2026-11-24 | Rate limiting for credential endpoints is in-process (token bucket per IP + scope) with the interface shaped for a Redis backend | Accepted, revisit before multi-replica production | With N replicas the effective allowance is N×. Not a blocker for single-instance deployment; the `allow(key)` seam is what a Redis limiter implements |
| D-024 | 2026-11-24 | Access tokens are validated with one indexed database lookup per request rather than self-contained JWTs | Accepted, revisit at scale | Revocation is immediate and there is no signing-key lifecycle to get wrong. A read-through cache (Valkey) is the planned optimisation when request volume justifies it |
| D-025 | 2026-11-24 | **Supersedes D-021**: password hashing is Argon2id (m=64 MiB, t=3, p=2, 32-byte key) via `golang.org/x/crypto/argon2`; PBKDF2-HMAC-SHA256 is kept read-only for legacy verification with rehash-on-login | Accepted | Memory hardness is the deciding property against offline GPU attacks. Full comparison, sizing and migration proof in `docs/security/password-hashing.md`; KAT pinned in `password_test.go` |
| D-026 | 2026-11-24 | TOTP secrets are stored AES-256-GCM encrypted with a key that is required outside `local`; the service answers 503 rather than storing plaintext when the key is absent | Accepted | A database dump alone yields no second factor. The key lives in the environment, not the database, so the dump and the key are separate compromises |
| D-027 | 2026-11-24 | MFA recovery codes: ten single-use 80-bit values, SHA-256 hashed, shown exactly once at enrolment | Accepted | Losing a phone must not mean losing an account, and support must not be able to reset a second factor on request |
| D-028 | 2026-11-24 | Roles are resolved from the database on every request instead of being embedded in the token | Accepted | A granted or revoked role takes effect immediately; the cost is one indexed join already needed for session validation |
| D-029 | 2026-11-24 | `roles.grant` and `admin.all` are admin-only by construction and are exempt from the "held by a non-admin role" rule | Accepted | Privilege escalation has exactly one owner; the matrix test now encodes that instead of forcing a synthetic grant |
| D-030 | 2026-11-24 | Email delivery goes through a `Sender` interface with an SMTP implementation (STARTTLS mandatory) and a log sender that refuses to be selected outside `local` | Accepted | Keeps CI hermetic without pretending to send mail, while production fails closed if SMTP is unconfigured |
| D-031 | 2026-11-24 | WebAuthn is implemented in-tree (verification only) on `fxamacker/cbor`, supporting `none`/`packed` attestation and ES256/RS256 rather than adopting a full server library | Accepted | We need verification, not attestation trust chains or metadata services. In-tree code is testable with a software authenticator and adds one small, permissively licensed dependency instead of a framework with its own session and RP abstractions |
| D-032 | 2026-11-24 | Passkey challenges are stored server-side, bound to the account for registration, and consumed by a single UPDATE | Accepted | Replay requires the server to remember what it issued; the atomic UPDATE makes concurrent replays of one capture impossible without a distributed lock |
| D-033 | 2026-11-24 | Rate limiting is a Redis/Valkey token bucket in one Lua script, and the limiter fails closed (503) when the store is unreachable | Accepted | The allowance must be global or it multiplies with replicas. Failing open would turn a Redis blip into unlimited credential guessing; production refuses to start with an unreachable store so the outage is found at deploy time |
| D-034 | 2026-11-24 | Coverage is measured with `-coverpkg=./...` so cross-package tests are attributed to the code they exercise | Accepted | The Redis limiter is exercised from `httpapi` tests; without `-coverpkg` it reported 0% while being fully tested |
| D-035 | 2026-11-24 | Access to paid content is granted only by a live row in `creator.entitlements`, with an in-period paid subscription as a second gate | Accepted | Two independent conditions mean neither a leftover grant nor a stale subscription row can unlock content. The database enforces one live entitlement per (fan, creator, scope) and one live subscription per (fan, creator) |
| D-036 | 2026-11-24 | Subscriptions snapshot the price and currency they were sold at; tiers are retired, never deleted | Accepted | A price change must not rewrite history, and existing subscribers must keep what they bought. Deactivating a tier stops new subscriptions while leaving the row for existing ones |
| D-037 | 2026-11-24 | Becoming a creator is the one self-service role grant; every other role grant stays admin-only and audited | Accepted | Publishing capability is not authority over money, moderation or other accounts. The grant is recorded as `role.self_granted` so a reviewer can tell it apart from an administrative grant |
| D-038 | 2026-11-24 | Without a payment provider the subscribe endpoint creates a `pending` subscription and answers 202 `payment_required`; only `ENABLE_FAKE_PAYMENTS` (refused in production) activates immediately | Accepted | Reporting an activated subscription with no charge would be a lie in the product and a hole in the ledger. The switch exists so CI can prove the entitlement path end to end |
| D-039 | 2026-11-24 | A configured shared rate-limit backend is treated as a required dependency by `/readyz` | Accepted | Logins fail closed when the limiter is down, so an instance that cannot reach it must leave the rotation instead of answering "ready" while refusing every credential request |
| D-040 | 2026-11-24 | Age verification is enforced server-side in the access decision, before any entitlement is considered, for every non-admin viewer including the creator | Accepted | A paid subscription must never substitute for verification, and a UI check is not a control. Ordering the gate before the entitlement lookup means there is no code path where billing alone opens 18+ content |
| D-041 | 2026-11-24 | Only `admin.all` bypasses the age gate, and the bypass is visible as the decision reason `admin` | Accepted | Support and moderation occasionally need to inspect a report. Keeping the bypass to one permission, with a distinguishable reason, keeps it auditable; every other role and the owner must verify |
| D-042 | 2026-11-24 | The verification result is stored, never the evidence: the schema has no column for documents, selfies, biometric templates or dates of birth | Accepted | Data we do not hold cannot leak. The vendor is the processor of record for the evidence; we keep a reference, an assurance label and timestamps |
| D-043 | 2026-11-24 | Provider callbacks are authenticated by an HMAC over the raw body with a 5-minute timestamp window, and applied at most once via `verify.callbacks` | Accepted | Session auth does not apply to a vendor webhook. Signed-plus-expiring stops capture replay; the unique event row stops a re-signed duplicate from extending a validity window |
| D-044 | 2026-11-24 | Verification stays unintegrated until a vendor is contracted: `POST /v1/verify/age/start` answers 503 and the gate denies paid scopes | Accepted (owner decision pending) | Starting a check requires a hosted session and a signed DPA. Returning 503 is honest; inventing a local "verified" path would be a fake control. Vendor shortlist and screening questions in `docs/vendors/age-verification.md` |
| D-045 | 2026-11-25 | Media storage keys are SHA-256 content hashes that the server computes; no client-supplied filename, path or content type ever reaches the filesystem or the database | Accepted | Traversal, collision-by-name and content-type confusion all disappear when the name is the hash of the bytes. Deduplication is a side benefit: identical bytes are stored once per instance and once per owner row |
| D-046 | 2026-11-25 | File type is decided by sniffing magic bytes (JPEG, PNG, MP4 with an allow-listed brand) and by `image.DecodeConfig` for dimensions; the `Content-Type` a client sends is ignored | Accepted | A browser header is a claim. A polyglot HTML/JS payload served with the wrong type is the classic upload-to-XSS path, so the bytes decide and `X-Content-Type-Options: nosniff` is set on the way out |
| D-047 | 2026-11-25 | A delivery link is `<jti>.<HMAC-SHA256(jti)>` and carries no authority: it names a row in `media.delivery_tokens` that is consumed by one atomic `UPDATE ... WHERE redeemed_at IS NULL AND expires_at > now()` | Accepted | The link must be a reference to a decision, not a capability. The UPDATE makes single use a database fact, so two concurrent redemptions cannot both win, and the token never encodes entitlements that could go stale |
| D-048 | 2026-11-25 | Redemption re-runs the full content authorization — age gate first, then entitlement/unlock — after consuming the token, and fails closed by leaving the token spent | Accepted | Cancellation and revocation between issue and redeem are the realistic attack. Re-checking means a link minted a minute ago cannot outlive the withdrawal of consent, and a partial failure burns the token rather than allowing a retry loop |
| D-049 | 2026-11-25 | Storage is a three-method interface (`Name`, `Put`, `Open`, plus `Stat`), with a disk driver shipped and an `Unavailable` driver that fails closed | Accepted (object store pending) | Zero-cost: the shipped driver needs no account. An S3-compatible store (MinIO, Garage, Cloudflare R2 free tier) implements the same interface later; the milestone must not depend on a paid bucket |
| D-050 | 2026-11-25 | No real payment gateway is integrated: none is adult-eligible, Egypt-accessible and genuinely zero-cost at the same time | Accepted (owner decision PAY-1..PAY-4) | High-risk processors permit adult content but charge annual card-brand registration (~$1,950/yr per the CCBill review) and a rolling reserve under contract; Egyptian acquirers require company KYB and do not permit adult content; mainstream gateways prohibit it. Evidence in `docs/vendors/payments-zero-cost.md` |
| D-051 | 2026-11-25 | Payments are behind a provider interface with an `Unavailable` driver that fails closed, and a fake driver that is refused in production | Accepted | Keeping the seam means the first contracted gateway adds one file, not a rewrite. Fail-closed means an unconfigured instance cannot accidentally sell something it cannot collect |
| D-052 | 2026-11-25 | The price of a purchase is always read from the tier or post record; the request body can choose *what* to buy and nothing else | Accepted | A client-supplied amount is how platforms get charged one cent for a $100 product. `CreateInput` has no price field, and the smoke test asserts the intent amount equals the stored price |
| D-053 | 2026-11-25 | Money is recorded only as balanced double-entry transactions in an append-only ledger; `UPDATE` and `DELETE` on ledger rows raise an exception, and balance is enforced by a deferred constraint trigger | Accepted | An editable ledger is not a ledger. Corrections are compensating transactions, and a single-leg posting cannot be committed even by a migration author who bypasses the service |
| D-054 | 2026-11-25 | `(kind, reference)` is unique on the ledger and `(provider, event_id)` is the primary key on payment events, so settlement and refund replays are no-ops rather than double postings | Accepted | Retries are normal. Both idempotency guards live in the database, so they hold under concurrency and across replicas, not just in one process |
| D-055 | 2026-11-25 | A refund reverses the ledger and revokes exactly what that intent granted (subscription, entitlement, unlock); partial refunds are refused | Accepted | Access is all-or-nothing per purchase. Half-refunding a subscription would leave an entitlement with no coherent price behind it |
| D-056 | 2026-11-25 | The platform share is `PAY_COMMISSION_BPS` (default 2000 = 20%) posted to `platform:revenue`, with the remainder to `creator:<id>:payable` | Accepted (placeholder until D-009 schedule is set) | The split must exist in the ledger from the first payment, otherwise the creator balance is unknowable. The rate is configuration, not code |
| D-057 | 2026-11-26 | Payments are NO-GO for an Egypt-based operator: no provider may be integrated until an operating entity in a lawful jurisdiction and a written offer exist | Accepted (owner decision PAY-1..PAY-4 superseded in scope) | Penal Code art. 178 (as amended by Law 189/2020) criminalises publishing obscene material for trade or distribution in Egypt, and Law 194/2020 art. 206 prohibits crypto settlement. Of the four reviewed leads, three settle only in crypto and the fourth (SegPay/CCBill-style fiat processors) boards US/UK/EU entities only. Evidence in `docs/vendors/payments-eligibility-review.md` |
| D-058 | 2026-11-26 | The development fake is restricted to `APP_ENV=local`: staging and production refuse to start with `ENABLE_FAKE_PAYMENTS` or `PAY_PROVIDER=fake`, unknown provider names are rejected, and `/v1/meta` publishes `payments.available=false` | Accepted | A simulated payment must never be mistaken for a real one. Enforcing it at configuration, at the intent endpoint (503, no row) and at the client keeps three independent layers between a test and a subscriber |

| D-059 | 2026-11-26 | Jurisdiction and operating location are separate facts: D-057's NO-GO applies to an Egypt-incorporated, Egypt-operated service, not to a foreign company with an Egypt-blocked product. The target structure is a company in Ireland, the UK or the US, managed and hosted outside Egypt, serving creators in US/UK/EU/CA/AU and subscribers everywhere except Egypt and prohibited territories, settling fiat through a processor that boards that jurisdiction | Accepted (owner correction, supersedes the scope of D-057) | Egyptian criminal law is territorial (Penal Code art. 1); art. 3 reaches an Egyptian acting abroad only where the act is also punishable where committed (dual criminality), and art. 2 reaches accomplice acts partly committed in Egypt. So the founder's residence is a residual risk for counsel, not a prohibition on the business. Evidence: `docs/vendors/jurisdiction-and-provider-assessment.md` |
| D-060 | 2026-11-26 | SegPay and CCBill are reinstated as primary payment candidates for an Ireland/UK/US company, subject to written confirmation of four unknowns: Egyptian-resident beneficial owner acceptance, the creator-platform commission model, creator payouts to US/UK/EU individuals, and fiat settlement to the company's bank in its own jurisdiction | Accepted (owner approval required before any provider contact) | Their published criteria (adult creator platforms as a named vertical, real subscription engine, fiat settlement to the merchant's bank, published card-scheme registrations) fit the model; only company-specific underwriting is unverified. Crypto-first providers stay rejected on settlement legality for an Egypt-resident founder |
| D-061 | 2026-11-26 | No payments, no provider integration and no launch until four gates close: incorporation jurisdiction, Egyptian counsel opinion on arts. 2 and 3 plus FX/tax, a written provider answer, and an adult-tolerant bank account in the chosen jurisdiction | Accepted | Each gate can independently disqualify a structure. Building the seam is finished; spending on registration or opening a merchant account before these are answered would be spending against an unknown outcome |

| D-062 | 2026-11-26 | The simulated payment provider is a first-class development and demonstration engine, not a stopgap to be hidden: it runs the real checkout, webhook, entitlement, ledger and creator-earnings code paths, in `APP_ENV=local` and in the new `APP_ENV=demo` environment only | Accepted (owner decision) | Product development must not stall on an unavailable gateway. The fake exercises the same service, database constraints and HTTP routes a real provider will use, so integration later is one driver plus one screen swap, not a redesign |
| D-063 | 2026-11-26 | Every simulated record is marked in the database: `payments.intents.simulated` and `ledger.transactions.simulated` are written at insert time, `/v1/meta` publishes `payments.simulated` and `payments.data_class`, and no client may render a simulated amount without a visible test label | Accepted | A label in the UI is not separation. If a test intent were indistinguishable from a real one in the table, the first real transaction would land next to hundreds of fake ones and every balance, payout and tax figure would be a guess |
| D-064 | 2026-11-26 | `APP_ENV=demo` is a fourth environment, distinct from staging: test payments are allowed there, in local, and nowhere else. Staging and production refuse both `PAY_PROVIDER=fake` and `ENABLE_FAKE_PAYMENTS` at boot | Accepted | Internal demonstrations need a real deployment with a real database, but staging is treated as a production rehearsal and must stay honest. A named demo environment gives demos a home without weakening the production guard |
| D-065 | 2026-11-26 | Real payment integration, and any production release, wait on the owner activating a provider in his own account and approving the integration; until then no paid capability is exposed and no real money path exists | Accepted (owner decision) | The launch gate is an owner action outside the codebase. Recording it here keeps the constraint visible to anyone reading the workboard |


| D-066 | 2026-11-26 | The Android messaging surface is wired to the real `/v1/dm/*` endpoints, and the product never states or implies end-to-end encryption: the thread carries an explicit note that messages are stored server-side and that this build has no E2EE | Accepted (owner instruction) | A stored-message platform cannot honestly claim E2EE, and a UI that stays silent invites the reader to assume it. The note is part of the screen, tested by `DmFormatTest.theThreadNoteDoesNotClaimEndToEndEncryption`, and no copy anywhere uses the words "encrypted" or "private" as a feature claim |
| D-067 | 2026-11-26 | `GET /v1/dm/conversations` carries `last_message_body` (flattened, 140 runes) and `unread_count`, so the inbox renders from one request instead of one request per thread | Accepted | An inbox that has to open every thread to show a preview is an N+1 by design and leaks read-state changes as a side effect of rendering a list. The count and preview are computed in the same query, and a multi-line body cannot stretch a row |
| D-068 | 2026-11-26 | A lapsed membership is a *send-only* failure: the thread stays readable, the composer is disabled with an explanation, and the server's 403 `subscription_required` is the same signal the UI renders | Accepted | Reading one's own history is not a subscription benefit, and losing it would make the product feel punitive. The API re-checks entitlement at write time, so the UI state and the server rule cannot drift apart |

| D-069 | 2026-11-26 | Tips are a payment purpose with their own ledger kind: `purpose='tip'`, `ledger.transactions.kind='tip'`, a bound of 100..500000 minor units and an optional note of at most 280 characters, and a tip grants nothing | Accepted | A tip is the only product where the client states an amount, because there is no price record to read. That makes the bound the control, so it lives in the service and is repeated by a database CHECK. Giving a tip its own ledger kind means an earnings or tax query can total tips without inferring them from a reference string |
| D-070 | 2026-11-26 | Every earnings figure is grouped by `simulated` and `GET /v1/creator/earnings` defaults to `include_simulated=false`; the excluded totals are still reported so a test balance is never silently dropped | Accepted | The fake provider writes real ledger rows (D-062), so one creator account holds both real and simulated money. A payout or a tax export that summed them would be wrong by an unknown amount, and an exclusion that hid its own existence would look like missing income. Gross comes from intents, net from the ledger, and neither is derived from the other |

| D-071 | 2026-11-26 | Device verification of the Android app is a GitHub-hosted emulator job that drives the installed app against a live API on the runner's loopback (`10.0.2.2:8080`), seeded by `cmd/e2eseed`, which refuses to run outside `APP_ENV=local`/`demo` with the fake provider | Accepted (owner approval) | Mocked UI tests prove a composable renders; they cannot prove the app logs in, sends, and reads a receipt. The API, its PostgreSQL service and the fixture exist only for the job, so no production record or real money path is reachable, and the app's networking is unchanged: the existing cleartext exception for `10.0.2.2` is what the emulator uses |
| D-072 | 2026-11-26 | The device suite is split around the membership ending: messaging, tips, theme and offline states run first, the workflow then expires the seeded membership with the same support action the seed tool performs, and a fourth run proves history survives while sending stops | Accepted | A single run cannot prove both halves of D-068 without waiting for a real subscription to lapse. Ending the membership between runs keeps each assertion honest: the same account, the same thread, only the entitlement changed |
| D-073 | 2026-11-26 | Safety reports live in `moderation.reports` with an append-only `moderation.report_events` trail; the event table refuses `UPDATE` and `DELETE` at the database level, and a closed report must carry `resolved_at` and `resolved_by` | Accepted | A moderation system is judged on its record, not its buttons. If a decision could be edited after the fact, an appeal, a regulator or a court could not rely on it. The constraint makes "forgot to record why" a schema error rather than a habit |
| D-074 | 2026-11-26 | Anyone with a session may report a subject they can actually see; only `safety.review` may read the queue or change a report; one open report per reporter and subject; and a moderator decision needs a note of at least three characters | Accepted | Reporting a private message you cannot see would turn the endpoint into an existence oracle, and duplicate submissions would let one person flood the queue. The note requirement is enforced in the service and shown in the UI, so the reason for a closure always exists |

## D-075 — Device tests address controls by what a screen reader would hear

Given, the emulator suite failed on ambiguous text ("You" is a tab and a message prefix,
"Messages" is a heading and a card). Decision: bottom-bar destinations carry
`contentDescription = "Tab <label>"`, and the tests tap the first node matching a label
rather than asserting uniqueness. Rationale: the assertion that matters is what happens
next, not how many copies of a word the tree contains, and the same handles make the shell
navigable by assistive technology.

## D-076 — Moderation is verified on a device, not only in Go tests

Given, the reporting UI and the moderator queue existed but had never run against a real
API. Decision: the emulator job files a report as a fan (including the duplicate collapse),
then signs in as a seeded moderator, moves the report to reviewing, and closes it with a
note; the job afterwards asserts through the API that the queue is a permission, that an
anonymous report is refused, and that an actioned report carries its closure stamp and a
three-event history. Rationale: a screen that has never submitted anything is not evidence.

## D-077 — A payout reserves money at approval, and only finance may decide

Given, a creator's payable balance is a ledger account and a withdrawal request is a
promise to move part of it. Decision: a request moves no money; approval moves it, in one
transaction that reads the live balance, posts a balanced `payout` transaction (debit the
creator's payable, credit `platform:payout_clearing`) and marks the request approved, so
two approvals cannot spend the same balance and a refund that arrives before the decision
makes the approval fail instead of overdrawing. A refusal of an already-approved request
posts a compensating transaction rather than editing history. One open request per creator
and currency is a partial unique index, and `payouts.request_events` refuses UPDATE and
DELETE. No endpoint sets `paid` or `failed`: no payout rail is configured, and the
platform does not report money as sent when it was not. Rationale: the ledger is the
source of truth for what is owed, so a reservation belongs in it, where the balance
trigger and the append-only rules already enforce honesty. Simulated money is excluded
from every balance read here (D-070), because the fake provider writes real ledger rows.

## D-078 — The tax export is a ledger statement, and it refuses rather than truncates

Given, a creator needs a file they can hand to an accountant or a tax authority,
and the dashboard answers "what did I earn" rather than "show me every movement".
Decision: `GET /v1/creator/earnings/export` returns CSV built from the ledger
transactions that touched the creator's payable account, one currency per file,
for an inclusive UTC date period bounded at 400 days. Each row carries
`occurred_at, kind, purpose, reference, currency, gross_minor, net_minor,
data_class`; a refund and a payout reservation are negative net lines, and gross
is set only where a payment intent backs the row. The excluded simulated row count
is returned in `X-Export-Excluded-Simulated` and every total is in `X-Export-*`
headers, so the file states what it leaves out (D-070). More than 5000 rows is a
400 asking for a narrower period, never a partial file. Rationale: a tax figure
must be reconstructible from the system of record, so the export reads the ledger
and not the dashboard's aggregates; a silently truncated year would be a return
that is quietly short.

## D-079 — The device fixture carries ledger money in both data classes, and only one undecided payout per currency

Given, the payout and statement surfaces need a balance to show, a reservation to
read and a test row to exclude, while `cmd/e2eseed` had until now written no money
at all. Decision: the seed creates two settled tips — one live (`simulated=false`,
recorded as fixture money in a disposable demo database, with no provider, card or
bank behind it) and one simulated (`simulated=true`, the data class the fake
provider produces) — then has the creator request a payout and finance approve it,
so the fixture ships a real reservation and its two-event trail. The seed reads the
numbers back from the ledger and the payout service, refuses to print a fixture
whose invariants do not hold, and remains unable to run outside `APP_ENV=demo|local`
with `PAY_PROVIDER=fake`. It also grants the creator role through
`BecomeCreator`, the audited self-service path the API uses: creating a profile
through the service alone leaves an account a fan at the API, which every
creator-only endpoint then refuses with 403. The same slice narrows D-077's "one open request" rule to
`status='requested'`: an approved request is already reserved in the ledger and
cannot be spent twice, so it must not block the creator's next request while a rail
pays it out. Rationale: evidence that a balance, a reservation and a statement work
has to come from the same paths the product uses, and a fixture whose numbers are
typed by hand proves nothing except that the author can subtract.

## D-080 — Studio audience numbers come from one creator-scoped query, and every money screen renders the API answer unmodified

Given, the approved studio mockup shows members, the share on the top tier and
7-day unlocks, and the previous Android studio drew them as fixed strings. Decision:
`GET /v1/creator/stats` answers from `creator` tables only — members are distinct
fans whose `active` subscription has not ended, per tier and summed once; the share
is basis points of that total and absent (nil) when there is no member; unlocks are
`source='purchase'`, unrevoked, granted in the last seven days on the creator's own
posts. The creator id comes from the authenticated principal, so the endpoint has no
parameter for reading another creator's audience, and admin/grant entitlements are
never counted. The Android `finance` layer parses the balance, the requests and the
CSV statement without arithmetic of its own: totals come from `X-Export-*` headers,
minors stay integers, and a missing figure renders as "—" rather than a plausible
number. Rationale: an audience tile is a claim about real subscriptions; a fixture
string on a production screen is a fabricated claim, and a client-side sum is a
second implementation of the ledger that can disagree with the first.

## D-081 — Owner is granted once, by one database path, and exactly one owner is a schema invariant

Given, the age-review queue is reachable only to an owner, and the repository had no
owner at all. Decision: `POST /v1/owner/activate` consumes deployment secret
`OWNER_ACTIVATION_SECRET` bound to one pre-designated account via
`OWNER_ACTIVATION_USER_ID` or `OWNER_ACTIVATION_EMAIL`, and requires an
email-verified session with a second-factor proof no older than five minutes.
Migration `0015` seeds slots 1 and 2, lets a slot go from unused to consumed exactly
once via a trigger, and adds a partial unique index `role_grants_one_owner` on
`identity.role_grants (role) WHERE role = 'owner'`. The service locks the selected
slot with `SELECT … FOR UPDATE`, checks for an existing owner, updates the slot,
then performs a plain `INSERT` of the grant. A unique violation rolls the whole
transaction back, including the slot, and records `owner.activation_denied` with
reason `existing_grant` in a separate transaction; `owner.activated` is written only
inside the committed transaction. Only slot 1 is configured; slot 2 stays dormant
and is exercised solely by the cross-slot race test. Rationale: a `count(*)` check
alone is not safe when two activations lock different slots, so the guarantee must
be enforced by the database. PostgreSQL superusers and privileged table owners can
still disable triggers; that access is restricted rather than claimed impossible.

## D-082 — Age review is manual, metadata-only, owner-only, and MFA-bound

The public age-verification provider path could not be purchased, and an uploaded-ID
flow would create a document-storage obligation the product does not want. Decision:
an account may open a manual review request (`POST /v1/verify/age/review`); the owner
sees the pending queue and can approve, reject or revoke. Approval writes a
`verify.checks` row with `method='manual_review'`, the configured validity window and
no evidence; rejection stores one of five fixed non-sensitive reason codes. The
schema has no document/blob/bytea column and migration 0016 refuses to apply if one
appears. Every action is authorized server-side by the owner role, `verify.review`
and a second-factor proof from the same session no older than five minutes. Nobody
may decide or revoke their own verification. Each request, approval, rejection and
revocation is appended to `ops.audit_log` in the same transaction as the state
change, and the audit hash is stored as the request's non-sensitive audit reference;
an audit failure rolls the decision back. Expiry is evaluated from `expires_at` on
every access decision, so a lapsed review loses access immediately without a
background job. Only slot 1 owner activation is configured; slot 2 stays dormant.
