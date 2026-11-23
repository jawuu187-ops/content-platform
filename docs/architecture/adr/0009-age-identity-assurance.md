# ADR-0009 — Age and identity assurance by default, with privacy-preserving attestations

**Status:** Accepted — 2026-11-23

## Context
Age assurance for adult surfaces is a legal requirement in a growing set of markets (UK
Online Safety Act duties enforced by Ofcom; roughly two dozen US states; EU member state
measures), and age estimation of uploaded content is necessary to detect minors. Biometric
age estimation is special-category data under GDPR, so architecture determines legal
exposure.

## Decision
1. **Verify by default, not by market.** A single global flow avoids compliance drifts and
   retrofits.
2. **Prefer privacy-preserving proofs.** Where a government-backed age credential/wallet
   exists (e.g. EU age-verification app / EUDI wallet), accept its attestation as the
   primary path: the platform learns "over 18" and nothing else. Fall back to a commercial
   vendor (ID document + selfie, or facial age estimation) with vendors that process in
   real time and do not store biometric templates.
3. **Content-level minor detection** on every uploaded asset before publication, plus hash
   matching for known illegal material and a mandatory reporting pipeline.
4. **Creator KYC is separate and stronger** than fan age assurance: government ID, liveness,
   address, sanctions/PEP screening, and tax forms (W-8BEN/W-9) before the first payout.
5. **Data minimisation in code:** the platform stores only a verification decision, a
   reference id, a timestamp and a vendor name — never a biometric template, never a
   document image.

## Consequences
- One vendor abstraction (`identity.AgeVerifier`) with multiple implementations.
- A DPIA is required before launch and must be updated when a vendor or flow changes.
- Verification cost is a per-user line item in the unit economics; reuse windows (e.g. 12
  months) are configurable per market.
