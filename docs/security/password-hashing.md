# Password hashing: decision record

Status: adopted 2026-11-24. Supersedes the PBKDF2-only choice recorded in D-021.

## The question

Which password hashing construction should the platform use, and how do existing
accounts move to it without a flag day?

## Candidates

| Construction | Memory hardness | Typical server cost | GPU/ASIC resistance | Library situation | Notes |
| --- | --- | --- | --- | --- | --- |
| PBKDF2-HMAC-SHA256, 600k iterations | None (uses a few KiB) | ~150-250 ms | Weak: an attacker parallelises on cheap cores and gains ~1000x over a CPU | Go standard library, zero dependencies | Wide compliance acceptance (FIPS 140 validated modules). Chosen in the first release because it needed no new dependency |
| scrypt (N=2^15, r=8, p=1) | Yes, 32 MiB per hash | ~100-200 ms | Good | `golang.org/x/crypto/scrypt` | Memory cost is fixed per call, so tuning for a shared host is coarse |
| Argon2id (m=64 MiB, t=3, p=2) | Yes, 64 MiB per hash | ~50-80 ms on one core | Best available: RFC 9106 recommended option, hybrid of data-independent first pass and data-dependent rest | `golang.org/x/crypto/argon2` (audited, used by Go's own ecosystem) | Requires an explicit memory budget per concurrent login |

## Decision

**Argon2id with m=64 MiB, t=3, p=2, 16-byte random salt, 32-byte output.**

Reasons, in order of weight:

1. **Memory hardness is the property that matters.** A stolen hash dump is attacked
   offline with GPUs. PBKDF2's cost is pure CPU, which scales almost linearly with
   the attacker's hardware. Argon2id forces 64 MiB per guess, so the same attack
   becomes memory-bandwidth bound and roughly an order of magnitude more
   expensive per unit of attacker spend.
2. **It is the algorithm RFC 9106 recommends** for password storage, and the
   construction the Password Hashing Competition selected.
3. **The cost is tunable and stored per hash.** Memory, iterations and parallelism
   live in the PHC string, so raising cost later is a configuration change plus a
   rehash-on-login, not a migration.
4. **The dependency is narrow and audited.** `golang.org/x/crypto/argon2` is the Go
   project's own implementation. Nothing else was added.

The original objection to Argon2id was "keeps the build dependency-free". That
concern is real but small: one well-scoped, officially maintained module, pinned
in `go.mod`, scanned by `govulncheck` and Trivy in CI. It does not justify a
weaker hash for the single most valuable credential on the platform.

## Sizing

* 64 MiB is ~2x the RFC 9106 minimum recommendation for the second recommended
  option (64 MiB, t=3) and fits comfortably in the container memory budget.
* Concurrency: each in-flight login holds 64 MiB for ~80 ms. At 50 logins/second
  that is ~4 GiB peak. The practical guard is the credential rate limiter, not the
  pool size, and if throughput grows the parameters are lowered deliberately with
  a documented measurement rather than silently.
* Test parameters are 8 MiB / t=1 / p=1 so CI stays fast; they are compiled in only
  through `identity.TestParams()` and are never reachable from configuration.

## Migration of existing hashes

* `Hasher.Verify` understands both `argon2id$...` and legacy `pbkdf2-sha256$...`.
* A successful legacy verification returns `needsRehash = true`. The login path
  then rewrites the row with an Argon2id hash in the same request.
* No user is forced to reset a password, and no window exists where credentials
  are unverifiable.

Verified by `TestLegacyPBKDF2HashVerifiesAndRequestsUpgrade` and
`TestVerifyFlagsWeakerParameters`.

## Verification

* `TestArgon2idKnownAnswer` pins the implementation against the published Argon2id
  v=19 vector for `password`/`somesalt`, m=65536, t=2, p=1. The expected tag was
  independently reproduced with OpenSSL's `ARGON2ID` KDF
  (`0d640df5...` is the RFC 9106 vector that needs a secret and associated data;
  that one cannot be used here because the Go API does not expose those inputs).
* Malformed, truncated and tampered PHC strings are rejected without panics.
* The full suite runs in GitHub Actions against PostgreSQL 18; nothing is executed
  on the development host.

## Rejected alternatives

* **bcrypt** — 72-byte input truncation, no memory hardness, and a 4 KiB block size
  that modern hardware handles trivially.
* **SHA-256 with a salt** — not a password hash at all.
* **Pepper / HMAC pre-hashing only** — useful as an addition (a key in the HSM/KMS)
  but not a substitute; deferred until a key-management service is chosen.
* **Argon2i or Argon2d** — Argon2i trades too much off-line resistance for side
  channel safety, Argon2d leaks memory access patterns. Argon2id is the hybrid.
