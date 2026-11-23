# ADR-0001 — Native Android client (Kotlin + Jetpack Compose)

**Status:** Accepted — 2026-11-23

## Context
The product's hardest client-side requirement is content protection: blocking screenshots
and screen recording, knowing when capture starts, playing DRM-encrypted video on secure
decoder surfaces, and reliably streaming large media on low-end devices. Distribution is
outside Google Play because store policy prohibits adult services.

## Decision
Build the first client as a native Android app in Kotlin with Jetpack Compose, Media3,
CameraX and Hilt.

## Alternatives considered
| Option | Why rejected |
| --- | --- |
| Flutter | Excellent velocity, but secure-surface/DRM control and Android capture-detection APIs are reached through platform channels; extra indirection exactly where we need certainty. |
| React Native (New Architecture) | Good ecosystem, but the same indirection, plus a JS engine on the media path. |
| Web/PWA only | Needed anyway as a channel and as the store-safe companion, but cannot reliably block screenshots or bind playback to a device. |

## Consequences
- Highest fidelity for protection, playback and performance.
- Larger client codebase and a specialised skill requirement.
- The web/PWA surface is still built, and a store-safe companion app remains part of the
  distribution plan (see ADR-0005 and the distribution section of the architecture doc).
