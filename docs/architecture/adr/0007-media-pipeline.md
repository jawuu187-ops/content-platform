# ADR-0007 — Media pipeline: direct upload, own transcoding, CDN-plural delivery

**Status:** Accepted — 2026-11-23

## Context
Cost modelling showed the delivery vendor choice dominates operating cost: the same
18-million-minutes-per-month workload costs roughly $3.3k on a per-GB CDN, $14.5k on a
per-minute video platform, and $18k+ on a per-minute platform with bandwidth bundled. The
choice is existential, not cosmetic.

## Decision
- **Upload:** resumable, direct-to-storage from the client using short-lived signed upload
  URLs. Media bytes never pass through the API.
- **Transcode:** per-minute transcoding service at low volume (fast to start); migrate the
  bulk of encoding to per-minute-billed managed transcoding with reserved capacity, or
  self-managed workers on spot capacity, once volume justifies it. A 4-rung ABR ladder
  (e.g. 240/480/720/1080p) with HLS as the default packaging.
- **Storage:** object storage with a zero-egress origin (Cloudflare R2 or Backblaze B2) for
  masters and renditions; lifecycle tiering for masters.
- **Delivery:** per-GB CDN (Bunny-class) as the default; a second CDN adapter kept warm for
  failover. Per-minute video platforms are not used above a small volume ceiling.
- **Anti-transcode thrash:** one media job per asset, idempotent, with deterministic
  rendition names.

## Consequences
- We own the bitrate/quality ladder, which is work, and we save an order of magnitude.
- Vendor adapters (`media.Transcoder`, `media.Delivery`) are mandatory so a provider swap is
  a configuration change plus a backfill job.
