# ADR-0008 — Content protection: layered deterrence with honest limits

**Status:** Accepted — 2026-11-23

## Context
Leak prevention is a core requirement, and marketing claims in this space are routinely
false. We need a strategy that states exactly what is prevented, what is deterred, and what
is impossible.

## Decision
Layered defence, with capability toggled per post by the creator where it costs something:

| Layer | Mechanism | Prevented? |
| --- | --- | --- |
| L1 | `FLAG_SECURE` on the player/message screen (creator-controlled per post) | Blocks screenshots and normal screen recording; window excluded from non-secure displays |
| L2 | Android capture callbacks: screen-capture start callback (API 34+) and recording notification (API 15+) to log and optionally pause playback | Detection, not prevention |
| L3 | Short-lived signed playback tokens (≤5 min), bound to session + device + IP class, revocable per user | Blocks link sharing and hotlinking |
| L4 | Concurrent-stream and device limits, session binding, tamper/root/emulator signals | Limits account sharing |
| L5 | Per-subscriber visible watermark (account id + timestamp) rendered into the delivered stream | Deterrence + attribution, survives re-encode |
| L6 | Leak detection (crawler + hash matching), DMCA automation, watermark-to-account correlation | Recovery, not prevention |
| L7 (later) | DRM (Widevine L3) for premium tiers; server-side per-session watermarking | Raises cost of piracy for high-value content |
| L8 | Image protection: signed delivery tokens, per-user watermarking, hotlink blocking | Deterrence |

**Impossible by nature (documented publicly so no one is misled):** camera recording of a
screen, rooted/emulated devices, and any DRM against a determined adversary. Protection is
deterrence + attribution + fast recovery.

## Consequences
- `FLAG_SECURE` can break casting and screen-share flows; it is therefore per-post opt-in
  for creators, with a platform-level default for paid content.
- Watermarking costs compute per session; visible overlay ships first, forensic
  server-side watermarking is a scale-stage investment.
