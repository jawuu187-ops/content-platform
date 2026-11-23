# Visual direction 03 — “Paper, not plastic.”

**Status: awaiting owner review of the refined board (26 Nov 2026).** Supersedes v2 as
the proposed direction. v2 and v1 remain in the repository untouched, for comparison.

This revision applies the refinement findings in [`refinement.md`](refinement.md):
seven type sizes instead of 35, six spacings instead of 20, 48 dp targets, drawn
chevrons instead of text glyphs, one reconciled dataset, a states section, and the
screen-specific fixes. It is still a proposal: **0 implemented, 0 partial, 18 proposed.**

Nothing here is implemented. The Android app is still the Compose shell described in
`review-v2/README.md`; no route, schema, dependency or production screen changed.

| Path | What it is |
| --- | --- |
| `board.png` | The direction, rendered end to end (1720 wide) |
| `board.html` | Board source: category read, identity, ink system, type, components, 18 screens, open questions |
| `screens/01…18-*.html` | One 412 × 915 dp screen per file |
| `png/01…18-*.png` | Screen renders at 824 × 1830 (2×) |
| `assets/app.css` | The system: 5 colour tokens, 3 typefaces, 8 components |
| `assets/img/hero.jpg` | Single photographic stand-in (generated; licensed photography is a separate decision) |
| `render.py` | Playwright renderer used by `.github/workflows/design-render-v3.yml` |
| `audit.py` | Re-measures type scale, spacing, targets, icons, states and figures |
| `refinement.md` | The ten findings and the screen-by-screen plan this revision follows |
| `category-alignment.md` | Comparison against the creator-subscription category, with the screen-by-screen recomposition proposal |

---

## 1. Why v2 failed, in one paragraph

v2 was a component deck: every surface a rounded card, every gap equal, gradients
standing in for photographs, serif used decoratively and an amber accent applied to
everything. It looked generated because nothing in it had been *composed* — there was no
subject, no ink, no editorial decision. v3 replaces the card library with rules and
ledgers, replaces decoration with one photographic subject, and makes money and names
typographic rather than badges.

## 2. The reference study (benchmark, not template)

| Product | What is professional about it | Taken | Rejected |
| --- | --- | --- | --- |
| OnlyFans | One unmistakable identity; price sits next to the action; creator page states plainly what a subscription buys | price adjacency, blunt copy | SaaS dashboard furniture, neon-blue identity |
| Fansly · Patreon | Dense tier tables scanned in one pass; benefits are typographic; states written in words | tier tables, word-based status | card-per-tier, pastel gradients |
| Substack | Editorial typography *is* the product; subscription framed as a sentence | type-led reading, text-first framing | newsletter furniture |
| Apple TV · MUBI | Media full-bleed; chrome recedes where content is the subject | full-bleed imagery, dark viewer only | cinematic black everywhere |

The conclusion: mature platforms are *quiet around the work and exact about money*. The
distinctiveness comes from a brand mark, one ink, and a typographic voice — not from a
palette.

## 3. Identity

- **Name:** Vesper (placeholder; brand decision D-006 is still open).
- **Mark:** an eclipse — an oxblood disc with a paper disc cutting it. One ink, survives at
  16 px, no gradient.
- **Wordmark:** didone capitals at 0.16 em tracking; the only widely tracked type in the app.
- **Voice:** short declaratives, no exclamation marks, prices always with currency and period.

**Ink system** (measured): ink `#191412` on paper `#F4EFE7` 15.9:1 · oxblood `#7A1F2B` 8.9:1 ·
muted `#6F635A` 5.1:1 · sage `#2F6B4F` 5.5:1 · paper on oxblood 8.9:1. Brass marks pending,
sage paid, red refusal; nothing else is coloured.

**Type:** Bodoni Moda (names, prices, pull quotes) · Archivo (everything touchable) ·
IBM Plex Mono (money, dates, ids, ledger rows, tabular). Scale 92/46/34/26/19/13.5/12/10.

**Structure:** 1 px rules at 16% ink (32% for section heads), 22 dp page margin, 3 dp button
radius, media corners square, 64 dp tab bar. Separation by rule and space, not by boxes.

## 4. Components (eight, no card library)

Actions (filled / outline / ink) · flags (subscribers, PPV, paid, pending, declined) ·
price-as-type · ledger rows · ruled list rows · single-underline fields · bottom sheet ·
skeleton and empty treatments.

## 5. Screens, with what is actually behind each

| # | Screen | UI | Backend reality |
| --- | --- | --- | --- |
| 01 | Splash | Proposed | none needed |
| 02 | Discover | Proposed | profiles + feeds exist; no search/trending |
| 03 | Creator profile | Proposed | profile, tiers, locked grid served |
| 04 | Membership | Proposed | tier price, perks, period served |
| 05 | Payment | Proposed | intents + ledger; sandbox, no live processor |
| 06 | Payment states | Proposed | intent status, events, replay guard |
| 07 | Feed | Proposed | feed + per-viewer lock flags; no reactions |
| 08 | Locked post | Proposed | access decision enforced server-side |
| 09 | Viewer | Proposed | single-use delivery live; visible watermark not built |
| 10 | Unlock sheet | Proposed | PPV unlock + refund revocation served |
| 11 | Library | Proposed | unlocks/entitlements exist; offline downloads do not |
| 12 | Account | Proposed | identity, security, verification served; no prefs API |
| 13 | Subscriptions | Proposed | list + cancel served |
| 14 | Statement | Proposed | history + ledger split served; no PDF receipt |
| 15 | Alerts | Proposed | no notification backend yet |
| 16 | Studio | Proposed | balance from ledger; aggregates and payouts missing |
| 17 | Composer | Proposed | upload, visibility, PPV price served; no scheduling |
| 18 | Tiers & audience | Proposed | CRUD + retirement served; per-tier counts not exposed |

**0 implemented · 0 partially implemented screens · 18 proposed.**

## 6. Navigation

Four destinations: **Home** (discovery and feed), **Library** (what you paid for),
**Create** (studio; hidden until the account publishes), **You** (account, billing,
verification). Pushed screens keep one back path. Unlock and confirmation are bottom
sheets. Money is never more than one step from the thing it buys, and a paid surface always
states which tier opens it and what it costs.

## 7. Review questions

1. Does paper / oxblood / didone / photography feel like the product?
2. Four destinations, studio inside Create — agreed?
3. One photographer-voice brand, or a neutral platform mark?
4. Should fans see the fee split (as the statement does), or only the total?
5. Anything missing before the token set and shell are built?

Approval must be given on the images. No UI work starts before it.


---

## 8. Sample data (one dataset, everywhere)

Every figure on every screen comes from this table. Nothing else may be typed on a screen.

| Tier | Price / month | Members | Monthly |
| --- | --- | --- | --- |
| Supporter | $5 | 4,120 | $20,600 |
| Studio | $9 | 6,310 | $56,790 |
| Collector | $19 | 1,982 | $37,658 |
| **Total** | | **12,412** | **$115,048** |

- Studio is **50.8%** of members and is the "most chosen" tier.
- Unlocks, last 7 days: **37** → **$888** (average $24 per unlock).
- Gross, last 7 days: **$27,732** (subscriptions pro-rated $26,844 + unlocks $888),
  average **$3,962/day**, best day **$4,910** (Tue).
- Fan-side commitment on screen 13: $9 + $5 + $12 = **$26.00**.
- A $9 subscription splits **$7.20 creator / $1.80 platform** (20%), inside the $9.

`audit.py` fails the "Numbers reconcile" section if a screen drifts from this table.

## 9. States

Loading, empty, failure, stale and offline are shown on the board as states of existing
screens, not as new destinations. Rules: skeletons match real geometry; an empty state
carries one action; a failure says what happened and what it cost; stale data says how old
it is; nothing opens without a live entitlement check.
