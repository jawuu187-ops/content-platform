# Threat Model

Method: STRIDE per trust boundary, plus abuse cases specific to this business. Reviewed
every release; owners are named per threat class in the risk register.

## Assets, in order of value

1. **Money** — ledger integrity, payout correctness, commission calculation, processor
   settlement reconciliation.
2. **Creator media** — unpublished masters and paid content.
3. **Subscriber PII** — identity data, verification references, payment metadata, purchase
   history, private messages.
4. **Credentials and sessions** — creator accounts are high-value takeover targets.
5. **Platform availability** — payment, delivery and upload paths.
6. **Reputation/legal** — moderation decisions, reporting obligations, transparency.

## Trust boundaries

| Boundary | Threats | Primary controls |
| --- | --- | --- |
| Client ↔ Edge | credential stuffing, token theft, replay, scraping, bot signups | passkeys/2FA, rate limits, device binding, bot scoring, anomaly detection |
| Edge ↔ Services | request forgery, header spoofing, SSRF | mTLS, signed internal requests, allow-listed egress, no user-controlled URLs server-side |
| Services ↔ Database | privilege escalation, data exfiltration, injection | least-privilege DB roles per module, parameterised queries only, no dynamic SQL from user input, row-level scoping by owner |
| Services ↔ Vendors | webhook forgery, vendor compromise, data leakage to vendor | signature verification, replay protection, minimal payloads, per-vendor allow-list |
| Admin ↔ Core | insider abuse, session hijack, privilege creep | separate IdP domain, hardware keys, IP allow-list, RBAC/ABAC, four-eyes, append-only audit |
| CI/CD ↔ Cloud | supply-chain compromise, secret exfiltration | GitHub OIDC (no long-lived cloud keys), pinned actions by SHA, SBOM, signed images, protected environments |
| Media delivery ↔ Fans | content leak, link sharing, account sharing | short-lived signed tokens, device/session binding, concurrency limits, watermarking, tamper detection |

## Abuse cases with controls

| Abuse | Control |
| --- | --- |
| Account sharing (one subscription, many viewers) | device registry, concurrent-stream limit, IP/ASN variance scoring, step-up verification |
| Mass scraping of public pages | rate limits, bot management, pagination caps, synthetic honeytokens |
| Content theft by a paying fan | per-user watermark + capture detection + attribution pipeline + account termination |
| Chargeback fraud ("friendly fraud") | prepaid wallet balances, clear billing descriptor, evidence bundle generator, dispute automation, 3DS where required |
| Promo/coupon abuse | per-user single use, device/identity linkage, velocity limits |
| Fake creators / stolen content | KYC before payouts, reverse-image and hash search at upload, originality attestation |
| Payout fraud / money mule | KYC, sanctions screening, payout velocity rules, bank-account ownership match, four-eyes above threshold |
| Refund abuse of consumed content | prepaid balance semantics, consumption tracking, refund policy with evidence |
| PII harvesting via messages | message limits for brand-new accounts, link filtering, report tooling |
| Insider access to private media | break-glass access with reason + alerting, no standing access to masters |

## Specific technical threats

- **IDOR / broken object-level authorization** on media, messages, purchases — every read
  path performs an explicit entitlement check; tests assert this per endpoint.
- **Signed-URL leakage** — tokens are per-user, per-asset, ≤5 minutes, bound to device,
  and revocable; token issuance is logged.
- **SSRF via remote media import** — disabled in v1; if added, requires an allow-list proxy.
- **Webhook forgery** (payment, moderation) — HMAC verification, timestamp window,
  idempotency keys, and a rejection counter alert.
- **SQL/migration drift** — migrations are versioned, applied only by CI/CD, and verified
  against a schema snapshot test.
- **Dependency supply chain** — Dependabot + Trivy + SBOM + image signing + pinned action
  SHAs.
- **Insider money manipulation** — immutable ledger, no UPDATE/DELETE on entries, four-eyes,
  anomaly alerts on manual journals.

## Residual risks accepted (documented, not hidden)

1. Camera recording of a screen cannot be prevented.
2. A rooted or emulated device can defeat client-side protections.
3. Some determined pirates will exfiltrate DRM-protected content.
4. Rolling reserves held by payment processors reduce available cash.
5. Vendor refusal risk for this content vertical remains a business continuity concern; the
   mitigation is multi-vendor architecture, not a contractual guarantee.
