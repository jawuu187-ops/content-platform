# Visual design review v2 — complete app journey

**Status: awaiting owner review. No Android or PWA UI work starts until this board is
approved in writing.** This directory is a design artefact only; it changes no application
code, route, schema or dependency.

| Path | What it is |
| --- | --- |
| `board.html` | The board source: direction, palette, type, journey, components, 20 screens, open questions |
| `board.png` | Full-page render of the board (rendered on a GitHub runner, committed back) |
| `screens/01…20-*.html` | One self-contained screen per file, 412 × 915 dp |
| `png/01…20-*.png` | Full-resolution renders of each screen (824 × 1830, 2×) |
| `assets/app.css` | The design system: tokens, components, screen chrome |
| `render.py` | Playwright renderer used by `.github/workflows/design-render.yml` |
| `../visual-board.*` | Board v1, kept for history; v2 supersedes it |

Render pipeline: `.github/workflows/design-render.yml` runs on `ubuntu-24.04`, renders with
Chromium, uploads the artefacts and commits the PNGs back. Nothing is built or rendered on
the owner's machine.

---

## 1. Implementation audit (from the repository, not from the backend)

The shipped Android app is a **shell**, not an app:

| Area | Status | Evidence |
| --- | --- | --- |
| Single Compose activity | Already implemented | `android/app/src/main/java/com/contentplatform/app/MainActivity.kt` |
| Window hardening (`FLAG_SECURE`, edge-to-edge) | Already implemented | same file |
| App shell: top bar, build identity, content-policy text | Partially implemented | `ui/PlatformApp.kt` |
| Theme + placeholder colour/type tokens | Partially implemented | `ui/theme/Color.kt`, `Type.kt` (plum/amber stand-ins, **not** this palette) |
| Navigation host, routes, deep links | Not implemented | no nav graph anywhere in the module |
| Any screen below | Not implemented | no Compose screen code exists |
| PWA / web client | Not implemented | no web target |

**Counts: 0 screens implemented · 0 partially implemented screens · 20 proposed.**

Backend capability is real and tested (identity, MFA, passkeys, creators, tiers,
subscriptions, age gate, media delivery, payments, ledger) — but an API is not a screen, so
no visual surface is claimed as implemented.

---

## 2. Screens and their status

`Proposed` = nothing exists. `Partial` = the screen does not exist but the endpoints behind
it do, with the stated gaps. No proposed mockup is described as an implemented screen.

| # | Screen | UI status | Backend reality today |
| --- | --- | --- | --- |
| 01 | Splash | Proposed | Client only; no API call |
| 02 | Onboarding | Proposed | Policy text served by `/v1/meta`; no onboarding-state API |
| 03 | Sign in | Proposed | Ready: password, TOTP, recovery codes, WebAuthn, rate limits |
| 04 | Age verification | Proposed | Partial: callback / status / revoke live; `start` answers 503 until a partner is contracted (D-044) |
| 05 | Home · discovery | Proposed | Partial: profiles and feeds exist; no search/trending/category endpoints |
| 06 | Creator profile | Proposed | Ready: profile, tiers, locked post grid |
| 07 | Tiers | Proposed | Ready: tier list with price, period, perks |
| 08 | Checkout | Proposed | Partial: intents, ledger, receipts; **no live processor** (sandbox only) |
| 09 | Payment states | Proposed | Partial: intent status + events; refunds are admin-only, full only |
| 10 | Subscriber feed | Proposed | Partial: feed with per-viewer lock flags; no likes or comments |
| 11 | Locked post | Proposed | Partial: access decision enforced server-side; blur is client-side |
| 12 | Protected viewer | Proposed | Partial: single-use delivery tokens live; **visible watermark not built** |
| 13 | PPV unlock | Proposed | Ready server-side: unlock, refund revocation, scope isolation |
| 14 | Profile & settings | Proposed | Partial: account/security/verification APIs; no preferences API |
| 15 | Subscriptions | Proposed | Ready: list, cancel at period end, immediate cancel for pending |
| 16 | Payments | Proposed | Partial: history + ledger split; no downloadable receipt |
| 17 | Alerts + states | Proposed | Not implemented: no notification backend at all |
| 18 | Creator dashboard | Proposed | Partial: balance from ledger; no aggregate stats; payouts disabled |
| 19 | Composer | Proposed | Ready: upload, hash storage, visibility, PPV price validation; no scheduling |
| 20 | Tier manager | Proposed | Partial: create/edit/retire; subscriber counts per tier not exposed |

Screens deliberately **not** drawn, because the product has no surface behind them yet:
direct messages, tips, payouts, moderation queue, admin console, search results.

---

## 3. Navigation and journey

Five tab destinations, chosen so that monetisation never hides discovery:

```
Home  ─ discovery, featured creators, continue watching
Subs  ─ the creators you already pay for, with renewal dates
Create─ creator studio (hidden until the account publishes)
Alerts─ activity, payment outcomes, verification results
You   ─ profile, verification, billing, settings
```

Pushed screens carry one back path; destructive or confirming steps are bottom sheets.

```
splash → onboarding (once) → auth
        ↘ age verification (blocking for paid surfaces only)
discovery → profile → tiers → checkout → payment state
feed → post → media viewer → PPV unlock sheet
you → subscriptions → payments → alerts
create → composer → tier manager
```

Three rules the journey enforces: a public profile and its public posts are always visible; a
paid surface requires **both** a live entitlement and an in-period subscription; and every
payment screen derives its state from the server, never from what the client thinks happened.

---

## 4. Design system

**Direction.** Dark, warm, quiet. Serif display against a neutral UI face. One accent (amber)
used only for money and verification; mint for success; red reserved for failure. Separation
comes from 1 px hairlines and background steps, not shadows.

**Palette.** Ink `#16101C`, surface `#241A2E`, line `#3A2C47`, text `#F4EFEA`, muted `#B9AEC0`,
amber `#E0A458`, amber-soft `#F2BE7A`, mint `#7FD1B9`, lavender `#9C8AC4`, danger `#E4695E`.
Measured contrast: body 16.3:1, muted 8.8:1, ink-on-amber 8.6:1, mint-on-ink 9.9:1.

**Type.** Fraunces (display, tier names, prices), Inter (UI, body, labels), JetBrains Mono
(amounts, ids, receipt lines). Scale: 34/27/21/16/14/12.5/11.5.

**Geometry.** 8 dp spacing base, radii 10/16/20/28, 20 dp page margin, 50 dp primary button,
78 dp tab bar, 56 dp app bar.

**Components.** Status pills (pending/paid/declined), tier cards (three tones), media tiles
with lock badges, watermark overlay, order summary with fee split, receipt rows, skeletons,
empty states, toasts, bottom sheets, switch, segmented tabs.

---

## 5. Existing vs proposed components

| Component | Exists today | Proposed here |
| --- | --- | --- |
| Theme tokens | Material 3 defaults + placeholder plum/amber | Full token set (colour, type, spacing, radius) |
| Buttons | Material `Button` | Filled / ghost / subtle / destructive, 50 dp |
| Cards | Material `Card` | Four tones incl. amber and danger |
| Inputs | none | Field with label, caret, focus ring, error |
| Navigation | none | 5-tab bar, segmented tabs, app bar, sheet |
| Media | none | Grid tiles, cover, lock badge, viewer, watermark |
| Status | chips in the shell | Pills, badges, toasts, skeletons, empty states |
| Money | none | Price block, order summary, receipt rows, tier cards |

---

## 6. Review checklist

1. Is the direction (dark, editorial, amber) the one to build?
2. Is five-tab navigation with Create hidden until publishing correct?
3. Is age verification a first-class screen (as drawn) or an inline gate?
4. Should fans see the fee split at checkout, or only the total?
5. What is missing before implementation starts?

Approval must be in writing on the actual images. A description of the board is not approval,
and no UI work begins until then.
