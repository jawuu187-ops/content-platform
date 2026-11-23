# Content Policy (product-enforced)

This document is the single source of truth for what may be published. It is implemented in
code (`internal/moderation`), not only in terms of service.

## Visibility classes

| Class | Audience | Permitted | Forbidden |
| --- | --- | --- | --- |
| `PUBLIC` | Anyone, including logged-out visitors | Non-nude imagery, teasers, promotional posts, behind-the-scenes, text | Nudity, sexual suggestion, lingerie-as-nudity substitution, anything 18+ suggestive |
| `SUBSCRIBERS` | Age-verified users with an active subscription or tier entitlement | Non-explicit nudity, suggestive posing, artistic nudity | Explicit sexual content of any kind |
| `PPV` | Age-verified users who purchased the item or unlocked the message | Same as `SUBSCRIBERS` | Same as `SUBSCRIBERS` |
| `PRIVATE_VAULT` | Creator only | Unpublished work in progress | Nothing is published from here without review |

## Absolute prohibitions (account termination + report)

1. Any depiction of a minor, real or simulated, or any content suggesting it.
2. Non-consensual intimate imagery, coercion, trafficking, or "leaked" content.
3. Explicit sexual conduct, penetration, or pornographic acts.
4. Sexual violence, bestiality, incest themes, or fetish content involving harm.
5. Content produced by third parties the creator cannot prove they have rights to.
6. Sale or promotion of illegal goods or services.

## Definitions used by moderators (to keep decisions consistent)

- **Non-explicit nudity:** visible nudity without sexual conduct — no genital focus for
  sexual gratification, no explicit acts.
- **Sexual suggestion:** posing, lingerie, implied context; allowed only in paid surfaces.
- **Explicit:** any act of sexual conduct, or imagery whose primary purpose is sexual
  gratification through depicted conduct. Prohibited everywhere without exception.

## Enforcement pipeline

1. **Pre-publication:** automated classification of every asset (nudity/explicit/minor
   signals). `PUBLIC` posts must pass a strict classifier; `SUBSCRIBERS`/`PPV` posts may
   pass with a lighter classifier and a human review queue.
2. **Publication:** content is visible only after classification completes successfully.
   `needs_review` states are invisible to fans.
3. **Human review:** queue with SLA (< 4 h p50, < 24 h p95), reason codes, and immutable
   decisions.
4. **Reports:** fan and creator reporting flows, with priority triage for
   minor-related and non-consensual reports (< 1 h).
5. **Appeals:** creator appeals with a documented reviewer, a second-level review, and a
   published outcome reason.
6. **Sanctions ladder:** warning → content removal → feature limits → payout hold (with
   documented reason) → suspension → termination. Payout holds require four-eyes approval.
7. **Reporting obligations:** illegal material is preserved and reported through the
   statutory pipeline with a one-year retention of reported items and metadata.

## Transparency

Publish (at minimum annually, and to creators on request): number of decisions, median
decision latency, appeal overturn rate, and the category breakdown of enforcement. This is
both a regulatory expectation and a competitive differentiator.
