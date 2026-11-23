# Media storage and CDN — options, cost and licences

Status: **evaluation only.** No account created, no bucket provisioned, no
contract signed, no vendor contacted. The shipped driver is local disk behind a
storage interface, so nothing in the product depends on this decision yet.

## What the platform needs

1. Durable object storage for images and short videos (10 MiB / 200 MiB limits).
2. Private by default: no public bucket, no permanent URL.
3. Cheap or free egress for review traffic.
4. An S3-compatible API so one adapter covers all options.
5. No vendor SDK in the module: signed URLs, if used, come from SigV4 we can
   implement on the standard library, keeping the dependency list unchanged.

## Zero-cost candidates

| Option | Licence / terms | Free capacity (verify before relying on it) | Fit |
| --- | --- | --- | --- |
| Local disk via `LocalStore` | in-house, no terms | bounded by the server disk | Default for CI and single-node deployments. No replication, no CDN |
| MinIO (self-hosted) | AGPL-3.0 (server) | your own hardware | Copyleft server, but it is infrastructure, not linked product code. Satisfies S3 API and private-by-default |
| Garage | AGPL-3.0 | your own hardware | Same trade-off, geo-distribution friendly |
| Cloudflare R2 | commercial, free tier + zero egress | free tier exists (10 GB) | Best cost profile for egress; paid beyond the tier, so it is a *later* option, not a free guarantee |
| Backblaze B2 + Cloudflare | commercial | small free tier | Cheaper than S3, but still a paid product at scale |
| AWS S3 | commercial | 5 GB trial only | Rejected: trail-based free tiers violate the zero-cost mandate |

## Decision direction

- Ship the interface and the disk driver now (done).
- Treat MinIO/Garage as the first production candidate because they are
  self-hostable and therefore genuinely free at zero scale.
- Keep R2 as the documented upgrade path when egress cost dominates; it is
  behind the same interface, so switching is configuration plus an adapter.
- Never serve objects directly from the bucket. Every byte goes through
  `/v1/media/delivery/{token}`, which re-checks the age gate and entitlement.

## Open questions for the owner

- MS-1: self-hosted object store (MinIO/Garage) versus managed free tier (R2)?
- MS-2: acceptable storage budget per creator before quotas are enforced?
- MS-3: is a CDN in front of the delivery endpoint acceptable given that it must
  never cache responses (`Cache-Control: private, no-store`)?

## Limitations of the current implementation

- Single-instance disk storage: no replication, no CDN, no lifecycle policy.
- No transcoding or thumbnail generation; a video is delivered as uploaded.
- No `Range` support, so video seeking is not available yet.
- `Sweep` (expired token cleanup) exists as a method but is not scheduled.
