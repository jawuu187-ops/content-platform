# ADR-0005 — Android builds run only on GitHub-hosted runners

**Status:** Accepted — 2026-11-23

## Context
The developer workstation is a Kali Linux machine used for security work. Installing the
Android SDK/Gradle toolchain there is undesirable, and local builds are not reproducible
with release artefacts anyway.

## Decision
Every Android compile, lint, unit test, instrumentation test and packaging step runs on
GitHub-hosted runners via GitHub Actions. The workstation contains the Android project
sources only — no SDK, no Gradle execution, no APK/AAB production. Self-hosted runners are
not used.

## Implementation
- `android-ci.yml` installs the Android SDK on the runner (`android-actions/setup-android`
  or the preinstalled toolchain), caches Gradle, runs `lint`, unit tests, `assembleDebug`
  and `bundleRelease`, and uploads APK/AAB as workflow artifacts.
- `release.yml` builds on tags, signs using secrets held in GitHub Actions, and publishes a
  GitHub Release with the artefacts.
- Pull requests cannot merge with a failing Android workflow; the CI job is a required
  status check.

## Consequences
- Builds are reproducible and auditable; artefacts are downloadable from Actions/Releases.
- Local iteration on Android is limited to code editing plus static reasoning; correctness
  is proven by CI. This is the accepted trade-off.
