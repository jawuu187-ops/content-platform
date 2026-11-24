# Device verification: what runs on a GitHub-hosted emulator

This is the evidence trail for the messaging, tips, theme, expiry and moderation work. It
exists because "it compiles" and "the unit tests pass" are not evidence that the shipped
application can log in, read a thread, send a message, show a receipt, or file a report.

Everything below runs on a GitHub-hosted `ubuntu-latest` runner. Nothing is verified on a
developer machine.

## The workflow

`.github/workflows/android-e2e.yml` starts a disposable PostgreSQL instance, builds the API,
runs the migrations, starts the API on the runner's loopback, seeds a fixture through
`backend/cmd/e2eseed`, boots an Android emulator, and drives the installed application
against that API at `http://10.0.2.2:8080`.

The seed tool refuses to run unless `APP_ENV` is `local` or `demo` **and** the payment
provider is the fake one, so the job cannot point at production data. Its fixture is one fan,
one published creator, one live membership granted by support (no subscription row, no
money), one conversation and one message. The fixture password is generated per run and
passed to the device as an instrumentation argument; no credential lives in the repository.

## What the device tests prove

| Test | Assertion |
| --- | --- |
| `MessagingFlowTest.signInInboxThreadAndSend` | Sign-in, inbox with preview and unread badge, thread with the no-encryption note, sending a message, and a preview refreshed from the server after the activity is recreated |
| `TipsFlowTest.presetsBoundsAndSimulatedReceipt` | Preset selection, the server's lower and upper bounds, a custom amount with a note, a receipt carrying the server's TEST label and reference, and no way to submit twice |
| `ThemeAndFailureTest.darkThemeIsPaintedAndStillSends` | Light and dark are actually painted differently, and sending still works in dark |
| `ThemeAndFailureTest.offlineStateExplainsItselfAndRecovers` | Airplane mode produces "You are offline", and Try again recovers |
| `ExpiredMembershipTest.readingSurvivesAndSendingIsPaused` | After the membership ends: read-only chip, "has ended" notice, history still readable, composer disabled |
| `ModerationFlowTest.aFanCanReportAMessageAndSeesTheReference` | Reporting a message, choosing a reason, adding detail, and receiving a reference from the server |
| `ModerationQueueTest.aModeratorReviewsAndClosesTheQueuedReport` | A moderator signs in, sees the queue, opens the report, moves it to reviewing and closes it with a note |

The suite is split around the membership expiry: the workflow ends the membership between
the second and third gradle invocation, because a shared fixture cannot be both live and
expired.

## What the API checks prove

After the tests, the same account calls the API directly, so a claim about the server is not
inferred from the screen:

- a 50-cent tip is refused with 400 (the sheet is not the only guard);
- one idempotency key sent twice returns the same `tip.id`, and the intent carries
  `simulated: true`;
- sending after the membership ended returns 403 `subscription_required`;
- a fan asking for creator earnings is refused with 403;
- a fan asking for the moderation queue is refused with 403, and an anonymous report is
  refused with 401;
- reporting the same subject twice returns 201 then 200, the second carrying the first
  report's id and `duplicate: true`;
- the report the device filed is `actioned`, has a `resolved_at` stamp, and its detail shows
  a history that includes the decision.

## Known limitations, stated plainly

- **Screenshots are unreliable on this AVD image.** `SurfaceFlinger` refuses pixel readback
  from inside the instrumented process (`captureToImage` and `uiAutomation.takeScreenshot`
  both return nothing), so the tests request screenshots from a host-side watcher that uses
  `adb screencap`. The watcher is best-effort and produced a partial set in the recorded
  runs; the honest evidence for those runs is the test XML, the semantics dumps printed on
  failure, and the API output. A complete, correctly ordered screenshot set still needs a
  different capture path.
- **The emulator needs `/dev/kvm`.** The job makes the device node usable before boot; without
  hardware acceleration the emulator refuses to start at all.
- **The emulator action runs every script line in its own shell.** Variables do not survive
  between lines, so a failing Gradle line fails the job on its own and no state is
  accumulated.
- **This is not a substitute for a real device.** Frame timings, OEM reduced-motion
  behaviour and the secure-flag screenshot block (`FLAG_SECURE`) are still unverified.

## Recorded runs

| Run | Result |
| --- | --- |
| `45351387665` | success — 7 device tests, all API checks, partial screenshots |
| `45349961294` | success — 7 device tests, screenshot capture incomplete |
| `45344250758` | failure — detail endpoint called on the queue route (fixed) |
