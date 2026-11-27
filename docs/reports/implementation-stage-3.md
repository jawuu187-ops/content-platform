# Implementation stage 3 — 2026-11-27

## Implemented

### Promotions, discounts and trials
- Creator-scoped promotion codes with percentage, redemption cap, expiry and
  activation.
- One redemption per fan, enforced by primary key; redemption count increments in
  the same simulated activation path.
- First-time free trial on a tier with `trial_days`; zero price, explicit
  `is_trial`, entitlement for the trial period.
- Simulated checkout applies the offer and never claims real collection.
- Android promo field and trial label.

### Privacy and account controls
- Versioned consent records (terms, privacy, analytics, marketing).
- Portable JSON export of account, creator profile, subscriptions, payments,
  messages and consents.
- Seven-day scheduled deletion, cancellable by the account.
- Retention worker anonymizes account email, password, profile and sessions;
  ledger/payment evidence is retained.
- Android privacy screen for consent, export preview and deletion.

### Safety and copyright
- `csam` report reason; queue orders CSAM first.
- CSAM reports write urgent outbox notifications to every owner.
- Copyright claims (post/message), counter-notice, moderator decision.
- Upheld post claim removes the post transactionally.
- No evidence images are accepted or stored.

### Analytics
- New/lost members over 30 days, active trials, published posts, likes and
  comments in the existing creator stats response.
- Android studio cards.

### Stories
- 24-hour expiring creator stories from owned media.
- Subscriber/owner/admin authorization, including media bytes.
- Android story publishing and subscriber story strip.

## Verification
- Full backend `go test -race -shuffle=on ./...`: pass.
- Backend CI 46014932330: pass.
- Security 46014932441: pass.
- Android CI 46014932381: pass.
- Disk checked before and after; disposable `cp_test_*` databases were dropped.
- Test harness now cleans stale, connection-free test databases before creating a
  new one.

## Not implemented
- Paid messages and message requests: the message domain still has no price,
  unlock or request-acceptance state. This is implementation work, not an
  infrastructure blocker.
- Live streaming: no SFU/TURN/RTMP media server or egress budget exists. Adding
  real live video would require infrastructure beyond the approved free-service
  constraints. Story-style expiring media is implemented; live is not claimed.
- No APK/AAB was produced; no production deployment or external provider was
  activated.
