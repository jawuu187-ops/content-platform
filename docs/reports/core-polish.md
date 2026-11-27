# Core platform polish — audio, profile customization, publishing

## Added

- Audio media support: MP3, M4A, OGG and WAV are validated by file signature,
  stored with the same ownership and delivery rules as images/video, and served
  through the authenticated range endpoint.
- Android authenticated audio playback with play/pause, duration and position.
- Creator welcome message on the profile, editable in creator tools and shown on
  the public profile.
- Draft and scheduled posts can be published immediately from the studio/vault;
  doing so clears `scheduled_at` so the database invariant stays valid.
- New media limit `MEDIA_MAX_AUDIO_BYTES` (default 60 MiB).

## Verification

- Full backend `go test -race -shuffle=on ./...`: pass.
- Backend CI 46026844845: pass.
- Security 46026844632: pass.
- Android CI 46026844889: pass.
- Disk checked before and after; disposable `cp_test_*` databases removed.

## Still open

- Paid messages and message requests.
- No live streaming, by owner decision.

No APK/AAB, production deployment, infrastructure change or external provider.
