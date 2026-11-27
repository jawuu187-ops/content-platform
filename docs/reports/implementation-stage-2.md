# Implementation stage 2 — 2026-11-27

This pass implemented stages 1–5 of the requested order. Stages 6–10 are not
claimed as complete.

## 1. Notifications and transactional outbox

- `notifications.outbox`, `notifications.items`, preferences and push-token tables.
- Database triggers enqueue events atomically for comments, likes, messages,
  subscription activation and successful simulated payments.
- `SKIP LOCKED` worker with retry/backoff, started by the API process.
- APIs: list, unread count, mark one/all read, get/update preferences, register
  and revoke push tokens. Email/push flags cannot be enabled without providers.
- Android notification centre with unread badge, mark-read and category
  preferences.
- Tests cover trigger creation, preference suppression, ownership of read
  operations and payment/message events.

## 2. Creator identity verification

- Metadata-only `creator.verification_requests`: legal name, country, date of
  birth, attestation, decision. A migration guard rejects any document/image
  column.
- Submission requires an existing profile and 18+ eligibility.
- Owner-only queue and approve/reject actions require `verify.review` and MFA
  within five minutes. Decisions update `creator.profiles.kyc_status` and audit.
- Tests cover approval, MFA denial, underage refusal and re-application rules.
- Android form and owner queue.

## 3. Profile media

- Avatar and cover are references to `media.objects`, never arbitrary URLs; the
  creator must own each image.
- Authenticated profile-image endpoint; draft profiles are visible only to owner.
- Android creator tools upload avatar/cover and the public profile renders both.
- Test covers ownership, public visibility and missing media.

## 4. Advanced messaging

- Image attachments with ownership checks, immutable messages and participant-only
  media reads.
- Creator broadcast to up to 1000 fans holding live entitlements.
- Android attachment upload/read and message rendering.
- Tests cover attachment authorization and broadcast count.

Not implemented in this stage: message requests and paid messages, because paid
messages require a subscription-like unlock record and a simulated checkout path
that is not yet wired into the message domain. They are not presented as done.

## 5. Scheduling, drafts and vault

- Posts support `scheduled` status, `scheduled_at`, and a database invariant
  requiring a time only for scheduled posts.
- API `GET /v1/creators/me/posts` returns drafts, scheduled and published posts.
- Scheduler in the API process publishes due posts idempotently.
- Android studio lists every status and offers Publish/Schedule.
- Test proves scheduled posts stay out of the feed until due and publish once.

## Verification

- Backend CI 46011887116: success.
- Security 46011887256: success.
- Android CI 46011475377: success.
- Android compile-only audit 46011473926: success.
- Local `go test -race -shuffle=on ./...` with PostgreSQL: pass.

No APK/AAB, production deployment, infrastructure change, real payment provider
or real email provider was used. The local disk exhaustion came from disposable
`cp_test_*` databases and was cleaned; no production database was touched.

## Still outstanding

Stages 6–10 remain: promotions/discounts/free trials, account deletion/export,
CSAM/DMCA workflows, deeper analytics, and stories/live. Paid messages and message
requests also remain. These require further implementation, not a policy decision.
