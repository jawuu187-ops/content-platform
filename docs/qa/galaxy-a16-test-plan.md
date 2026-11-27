# Real-device test plan: Samsung Galaxy A16 5G (Android 16)

Build under test: `com.contentplatform.app`, version `0.2.0-test` (code 2), signed
testing APK from GitHub Actions run `46033928821`.

Record each result as PASS / FAIL / BLOCKED with a screenshot where useful. A CI pass is
not device evidence.

## 0. Install and environment

- [ ] Download `content-platform-testing-apk` from run `46033928821`.
- [ ] Verify SHA-256 is `bd29b86466715ce43aae4906c3ac23861790876c2fb2dd3a91710c62c2c5fd81`.
- [ ] Allow install from the browser/file manager, install, and confirm the app label.
- [ ] Confirm Settings → Apps shows version `0.2.0-test`.
- [ ] Confirm the app reaches `https://80.47.235.113.sslip.io` (registration screen,
      no localhost error).
- [ ] Rotate the device and background/foreground the app; session should survive.

## 1. Registration, login, sessions

- [ ] Register a new email and a strong password; the app shows email-verification
      (simulated mail) and the account is usable after postponing.
- [ ] Sign out and sign in again.
- [ ] Force-stop the app and reopen; session is restored.
- [ ] Enter a wrong password: clear error, no crash, no fake success.
- [ ] Request a password reset; confirm the response says simulated mail.
- [ ] Enable TOTP in Security, sign out, sign in with the code; recovery codes shown.
- [ ] Sign out from Security; protected screens become unavailable.

## 2. Discovery and navigation

- [ ] Home lists real creators from the server; search and category chips work.
- [ ] Load more appends without duplicates.
- [ ] Open a creator; back returns to the same feed position.
- [ ] Bottom tabs Home / Tiers / Studio / You preserve state.
- [ ] Airplane mode: each screen shows an error with retry, not blank or invented data.

## 3. Creator onboarding and tools

- [ ] Create a creator profile (handle, name, bio, category), publish it.
- [ ] Confirm it appears in Discover from a second account.
- [ ] Upload avatar and cover; they render after reload.
- [ ] Create/edit/deactivate a tier; price and period survive reload.
- [ ] Upload an image, a video and an audio file; each post renders its media.
- [ ] Create a public post, a subscribers-only post and a PPV post.
- [ ] Schedule a post and confirm it appears only when due.
- [ ] Publish a saved draft.
- [ ] Add a story and confirm it appears in the profile strip.
- [ ] Set a welcome message and confirm it appears on the public profile.
- [ ] Open creator verification and submit metadata; owner queue shows it (owner only).
- [ ] Open promotions and confirm create/disable works (after backend 17–30 deploy).

## 4. Fan membership and payments

- [ ] Open a paid tier and request membership; clearly labelled simulated checkout.
- [ ] Membership becomes active immediately for the fake provider; access is granted.
- [ ] Cancel; membership remains readable until period end and shows cancellation.
- [ ] Create/activate a free trial tier; trial label and dates are correct.
- [ ] Apply a promotion code; discount is reflected and reuse is refused.
- [ ] Unlock a PPV post; content opens and the UI says simulated / no real money.
- [ ] Open Payment history; entries are marked simulated and match the actions.
- [ ] No screen claims real money was charged, earned or withdrawable.

## 5. Messaging and paid messages

- [ ] Subscriber opens a conversation and sends text.
- [ ] Send an image attachment; recipient sees it.
- [ ] Creator sends a broadcast; subscribers receive it.
- [ ] Non-subscriber sends a request when policy is `everyone`; creator sees pending.
- [ ] Request attachment is hidden until accept.
- [ ] Creator accepts; both can message. Reject/block hides the attachment.
- [ ] Creator enables paid messages with a default price; send a locked message.
- [ ] Fan sees "Locked paid message" with no body/media, then unlocks; content appears.
- [ ] Replay/retry does not create a second charge or duplicate receipt.
- [ ] Owner/admin account cannot open private messages without being a participant.

## 6. Posts, engagement and notifications

- [ ] Like/unlike, bookmark/unbookmark, comment/delete own comment.
- [ ] Bookmark list shows saved posts.
- [ ] Locked posts refuse like/comment until unlocked.
- [ ] Notification badge increments for a like/comment/message/payment.
- [ ] Notification list opens and marks read; unread count falls.
- [ ] Notification preferences persist.
- [ ] After backend deploy: request/accept/paid-message events appear.

## 7. Media playback

- [ ] Image post opens full resolution and rotates correctly.
- [ ] Video plays, pauses, seeks, and shows progress; audio continues correctly.
- [ ] Audio post plays/pauses and reports duration.
- [ ] Media on a locked post is not downloadable before unlock.
- [ ] Rotate during playback; no crash, no double audio.

## 8. Error handling and safety

- [ ] Kill network during login, upload, subscribe, unlock, send, and unlock-message:
      each shows a retry/error, never a success state.
- [ ] Expired session: app refreshes silently or asks to sign in.
- [ ] Server 404 for a not-yet-deployed feature is shown as an error, not a crash.
- [ ] Age gate: under-18/unknown account cannot open adult-gated content.
- [ ] Report a post/message; report creates a moderation item.
- [ ] Account export downloads and consent toggles save.
- [ ] Schedule account deletion and cancel it; both states are visible.

## 9. Device-specific

- [ ] Android 16 predictive back and system back behave correctly on every pushed screen.
- [ ] Keyboard/IME does not cover the message composer or checkout buttons.
- [ ] Notch/status bar and navigation bar insets are correct.
- [ ] Screen capture is blocked where FLAG_SECURE is applied.
- [ ] Battery/background: notification polling does not prevent sleep or drain visibly.

## Result log

```
Date:
Device:
Build:
Backend commit / migration:
Tester:
PASS:
FAIL:
BLOCKED:
Screenshots:
```
