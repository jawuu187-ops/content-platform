# Implementation status — 2026-11-27

This report covers the implementation pass that followed the competitive audit.
It is deliberately evidence-based: every "implemented" item below has application
logic, database or API support, and a test or a hosted CI run. Nothing was
deployed to production and no APK/AAB was built.

## Gates

| Gate | Result |
| --- | --- |
| Android compile, JVM unit tests, lint (GitHub Actions) | pass, run `46004956853` |
| Android compile-only audit (no packaging) | pass, run `46004956225` |
| Backend format, vet, race tests against PostgreSQL (CI) | pass, run `46005377064` |
| Security (gitleaks, staticcheck, govulncheck, Trivy, gosec) | pass, run `46004956963` |
| Local `go test -race -shuffle=on ./...` with PostgreSQL | pass |

Android CI and the E2E workflow no longer run on push and no longer package an
APK or AAB. The E2E workflow is manual only and remains intentionally unrun
because it would install a debug build.

## Implemented in this pass

### Simulated email and account recovery

- `backend/internal/platform/mailsim` persists every simulated message in
  `mail.simulated_messages`; it never logs a body and never uses a network.
- `MAIL_PROVIDER=simulated` is accepted only with `APP_ENV=local` or `demo`.
- `backend/cmd/mailsim` lists the local mailbox for a developer. It requires a
  direct database URL and is not an HTTP endpoint.
- Password reset request/confirm endpoints:
  - `POST /v1/auth/password/reset/request` always answers `202` and does not
    reveal whether an account exists.
  - `POST /v1/auth/password/reset/confirm` consumes a single-use, 30-minute token,
    replaces the Argon2id hash, revokes every session and writes an audit event.
- Reset tokens are stored only as SHA-256 hashes. Neither token nor reset body is
  returned by a public API.
- Tests cover the full hash/session replacement, single use, and unknown-address
  non-disclosure.
- Android has a complete forgot-password request and reset-code flow.

### Post engagement

- Likes, bookmarks and comments with schema constraints and indexes.
- APIs:
  - `POST|DELETE /v1/posts/{id}/like`
  - `POST|DELETE /v1/posts/{id}/bookmark`
  - `GET /v1/me/bookmarks`
  - `GET|POST /v1/posts/{id}/comments`
  - `DELETE /v1/comments/{id}`
- Likes and comments require the same access decision as the post; a locked post
  returns `403`. Bookmarks are allowed on locked posts and remain locked when
  listed.
- Comment deletion is limited to the comment author, the post creator, or an
  administrator; deletion is soft and audited.
- Android post rows show counts, like/save actions, comments, comment creation
  and deletion with loading/error states.

### Creator discovery

- Category filter and offset pagination:
  `GET /v1/creators?q=&category=&limit=&offset=`.
- Response includes `next_offset` (`-1` at the end).
- Unknown categories are ignored rather than passed through to SQL.
- Android has category chips, a search action, and a Load more control.
- Test covers category filtering, page boundaries and text search.

### CI and correctness fixes

- Android compile errors fixed (missing `ApiException`, missing shared `money`
  formatters, unresolved imports).
- Backend owner-activation test now scopes denial audits to the two users in the
  test instead of counting audit rows from earlier shuffled tests.
- Gitleaks configuration allowlists only the named test fixtures and the smoke
  callback secret; staticcheck's unused type was removed.
- Migrations grant privileges conditionally, so a clean CI database without the
  `content_api` role migrates successfully.

## Existing functionality re-verified

Authentication, email verification, MFA/step-up, creator profiles and tiers,
subscriptions, PPV unlock, authenticated media playback, direct messages,
creator tools, earnings, ledger, payout review/settlement, age gate, manual age
review, owner activation and reporting still compile and pass their tests.

## Still not implemented / blocked

These audit gaps are **not** claimed as done:

- Push notifications and in-app notification centre/preferences.
- Creator KYC/identity verification beyond the approved manual age review.
- Avatar/cover/profile-media upload UI (the profile API already stores URLs).
- DM media, paid messages, message requests and mass messaging.
- Scheduled posts, drafts/vault UI, bulk upload, stories and live streaming.
- Promotions, discounts and free trials.
- Account deletion, data export, consent management.
- Creator audience analytics beyond existing aggregates.
- CSAM/DMCA-specific workflows, object storage/CDN, background worker, email
  provider delivery.
- The explicit-content policy conflict remains unchanged and requires an owner
  decision.

## Constraints preserved

- No production deployment, DNS, firewall, port, database or media-storage
  change.
- No real payment or email provider activated.
- No document upload or storage for age verification.
- No APK or AAB produced.
- Simulated money and mail remain explicitly labelled as simulated.
