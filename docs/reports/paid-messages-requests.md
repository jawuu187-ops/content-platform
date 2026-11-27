# Paid Messages and Message Requests

Status: implemented, simulated payments only. No production deployment.

## Scope

- Creators choose a request policy (`subscribers`, `everyone`, `none`), turn paid
  messages on or off, and set a default price between 100 and 100000 minor units.
- A fan without a subscription may send one request when policy is `everyone`.
  Requests carry text plus at most one attachment. Acceptance grants messaging in
  that conversation only, never subscriptions, posts, stories or entitlements.
- Rejected or blocked requests retain no accessible attachment. The retention worker
  purges attachment bytes after 30 days and nulls the message media reference. The
  message text remains as moderation evidence.
- Paid messages are locked server-side. Body, preview, media id and attachment
  metadata are withheld from fans until a live unlock exists.

## Data model (migration 0030)

- `creator.profiles`: `dm_request_policy`, `paid_messages_enabled`,
  `default_message_price_minor`, `message_currency`.
- `messaging.conversations`: `status` (`requested`, `accepted`, `rejected`,
  `blocked`), `initiated_by`, `requested_at`, `responded_at`.
- `messaging.blocks`: bilateral denial, no self-block.
- `messaging.messages`: `price_minor`, `currency`, `unlock_required` with a bounded
  price check.
- `messaging.message_unlocks`: append-only history. A partial unique index
  `(message_id, fan_id) WHERE revoked_at IS NULL` allows a new purchase after a
  refund while retaining every prior row. `payment_intent_id` is unique.
- `messaging.request_attachments`: `purge_after` mark for rejected/blocked media.
- `payments.intents`: `message_id` plus purpose `message` and a target check that
  requires the message and creator.

## API

| Method | Path | Purpose |
| --- | --- | --- |
| POST | `/v1/dm/requests` | fan request (`creator_id`, `body`, optional `media_id`) |
| GET | `/v1/dm/requests` | creator-only pending request list |
| POST | `/v1/dm/conversations/{id}/accept` | creator accepts, grants conversation messaging |
| POST | `/v1/dm/conversations/{id}/reject` | creator rejects, marks attachment purge |
| POST | `/v1/dm/conversations/{id}/block` | creator blocks, marks attachment purge |
| POST | `/v1/dm/messages/{id}/unlock` | idempotent simulated checkout for one locked message |
| GET/PATCH | `/v1/dm/settings` | creator request/paid-message settings |

`POST /v1/dm/messages/{id}/unlock` accepts `idempotency_key` (8-128 chars). It creates a
`payments.intents` row with purpose `message`, settles it through the fake provider,
and returns the unlocked message. A replay with the same key returns the same intent.

## Concurrency and refunds

- Settlement inserts the live unlock before marking the intent succeeded or posting
  the ledger entry. A second intent for the same message fails with
  `ErrAlreadyUnlocked`, so two different idempotency keys cannot double-charge.
- The same key always returns the existing intent, including after the message is
  already unlocked.
- Refunds are recorded and the ledger is posted before `RevokeMessagePaid` marks the
  live unlock revoked. The fan's thread re-locks immediately, and a later purchase
  inserts a new unlock row while the old row stays in history.

## Moderation and privacy boundary

- There is no admin or owner endpoint that returns DM contents. Messaging queries
  require the caller to be one of the two participants.
- No exceptional moderation access was added. If one is ever added it must be
  explicit, time-bound, justified and write an `ops.audit` record before returning
  any content.
- Locked previews are replaced with `Locked paid message`; locked bodies and media
  are blanked in list and thread responses. Request attachments are hidden until the
  creator accepts.

## Verification

- `go test ./...` with `TEST_DATABASE_URL` pointed at a disposable database.
- New tests: `TestPaidMessageRedactionAndRequestLifecycle`,
  `TestRejectedRequestAttachmentPurgeMarker`,
  `TestPaidMessageUnlockIdempotencyAndRefundReunlock`,
  `TestConcurrentPaidMessageSettlementsCannotDoubleCharge`.
- Android: compile/test/lint on GitHub Actions only; no APK/AAB is built.

## Limitations

- All payments are simulated. No card data, provider webhook or real money is used.
- The retention worker runs every six hours in the API process; it is not a separate
  durable job system.
- Fan request UI requires the creator policy to be `everyone`; there is no request
  composer for `subscribers`.
- Paid-message composer uses the creator's default price; per-message custom pricing
  is not exposed in Android yet.
