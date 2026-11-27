# Android integration audit and testing build

Date: 2026-11-27. No production deployment, migration or infrastructure change was made.

## Current deployed backend

- Public API: `https://80.47.235.113.sslip.io`
- `/version` reports `{"version":"dev","commit":"none"}` because the deployed binary was
  built without version metadata.
- Database `content_platform` has migrations through **16** in
  `public.schema_migrations`.
- The running API binary is from 07:39 and predates the merged 17–30 features.

Probes of the deployed API:

| Endpoint | Deployed | Latest code |
| --- | --- | --- |
| `/v1/creator/stats` | 401 (route exists) | exists |
| `/v1/owner/verify/reviews` | 401 (route exists) | exists |
| `/v1/notifications` | 404 | exists |
| `/v1/creators/me/posts` | 404 | exists |
| `/v1/dm/settings` | 404 | exists |
| `/v1/dm/requests` | 404 | exists |
| `/v1/me/export` | 404 | exists |
| `/v1/posts/{id}/comments` | 404 | exists |

With migration 16 deployed, the APK can use auth, base creator profiles, base posts,
base media, subscriptions, PPV, base DMs and payouts. It cannot use notifications,
profile media, DM attachments, scheduled/draft posts, promotions, privacy rights,
creator KYC, stories, audio media, welcome messages, engagement, paid messages or
message requests until the backend is updated.

## What was integration-checked and fixed

- Android route inventory was compared with `backend/internal/httpapi` route
  registrations. Every Android call maps to an existing latest-code route.
- Simulated tips previously showed a "Tip sent" receipt while the intent was still
  `pending`. The app now completes the fake-provider intent through the owner-only
  `POST /v1/payments/{id}/simulate` route and displays the settled receipt. The route
  refuses non-simulated intents and intents owned by another account.
- The testing APK is now built with an explicit release signing configuration and a
  workflow that fails if signing, package identity, version or HTTPS endpoint checks
  fail.
- Version raised for the test build: `versionCode 2`, `versionName 0.2.0-test`.
- Package: `com.contentplatform.app`.

No mock data or fake success state was added. Existing demo-only screens are not used
by the current navigation (`PlatformApp` uses the API-backed screens).

## Migration rehearsal (safe, not production)

A byte-for-byte clone of the production database was created locally, restored from a
`pg_dump`, and migrated with the latest migrator:

```
migrations applied versions="[17 18 19 20 21 22 23 24 25 26 27 28 29 30]"
max(version)=30 count=30
```

The clone was dropped immediately afterwards. Production was only read, never written.
This proves the exact pending migration set and that the schema applies cleanly over the
current production schema.

## Testing APK

- GitHub Actions run: **46033928821** (Android test APK, success)
- Artifact: `content-platform-testing-apk`
- APK: `app-release.apk`
- Package: `com.contentplatform.app`
- Version: `0.2.0-test`, versionCode `2`
- minSdk 26, targetSdk 36 (Android 16)
- Signed: APK Signature Scheme v2, 1 signer
- Signer: `CN=Content Platform Release, OU=Android, O=Content Platform, L=Amsterdam, C=NL`
- Certificate SHA-256: `50f033894b06497f78adb0d02e97b37d394cc3c2983485342a66bda089fbc95f`
- APK SHA-256: `bd29b86466715ce43aae4906c3ac23861790876c2fb2dd3a91710c62c2c5fd81`
- API baked in: `https://80.47.235.113.sslip.io`
- No AAB was produced.

The workflow constructs the signed APK on `ubuntu-latest` only. Its build asserts no
AAB exists.

## Important limitation for the first device session

The APK is newer than the deployed backend. On the current backend it will prove auth,
discovery, base creator pages, uploads, posts, subscriptions, PPV and base messaging,
but newer screens will receive 404. Do not interpret those 404s as device or client
failures. The deployment plan is in
`docs/operations/deploy-17-30-plan.md` and requires separate owner approval.

## Evidence not claimed

- No feature on this page is claimed to work from CI alone.
- The APK was not installed on the physical Samsung device by this agent.
- Real-device results must be recorded from the checklist in
  `docs/qa/galaxy-a16-test-plan.md`.

## Production update executed (owner-authorized)

- Backup: `content_platform_pre30_20261127T170655Z.dump`, SHA-256
  `d2177e68a84a3feae34ca521362dc78178ee3ea8d788247057569e81e0079478`.
- Restore rehearsal: restored into `cp_backup_verify`, schema 16 and row counts matched
  (`identity.users=12`, `creator.posts=4`), then the verification database was dropped.
- Migrations applied: `[17 18 19 20 21 22 23 24 25 26 27 28 29 30]`.
- Final schema: max version 30, 30 recorded migrations.
- Deployed API: `0.2.0-test`, commit `33325a7`, built `2026-11-27T17:07:07Z`.
- Runtime endpoint remains `https://80.47.235.113.sslip.io`; no port, DNS, firewall,
  Caddy, provider or environment-policy change.
- `/readyz`: database ok, version 0.2.0-test.
- `/v1/meta`: fake/simulated payments still labelled.
- Route probes now return 401 rather than 404 for notifications, creator posts, DM
  settings/requests, privacy export/consents, earnings export and comments.
- `POST /v1/payments/{id}/simulate` returns 401 unauthenticated (route exists).

## Fresh testing APK after the backend update

- GitHub Actions run: **46039547209** (success)
- Artifact: `content-platform-testing-apk`
- Version: `0.2.1-test`, versionCode `3`
- Package: `com.contentplatform.app`
- APK SHA-256: `147eccbef6ffead928646f3b6684a028d15336d7d7450f684e76835665709a9f`
- Certificate SHA-256: `50f033894b06497f78adb0d02e97b37d394cc3c2983485342a66bda089fbc95f`
- Signed with APK Signature Scheme v2.
- API baked in: `https://80.47.235.113.sslip.io`
- No AAB produced.
