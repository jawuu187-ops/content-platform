# Recomposition — rounds A–F

**Status: complete, rendered once, awaiting the owner's review of the images.** This is the
record of what changed after the five category-alignment decisions were approved. It
supersedes nothing: the identity from direction v3 is unchanged, the composition is new.
Backend, Android code, migrations and schemas were not touched.

Benchmark: **OnlyFans is the primary product benchmark.** Other products in
[`category-alignment.md`](category-alignment.md) are secondary evidence. Branding,
palette, type and mark remain ours.

---

## The five approved decisions, as built

| # | Decision | Where it shows |
| --- | --- | --- |
| 1 | Photography occupies at least half of the first screenful; typography steps back | 02, 03, 07, 11 |
| 2 | Bone stays as brand chrome; browsing sits on a warmer near-white; media cards separate | whole system (`assets/app.css`) |
| 3 | Price inside or next to the primary subscription action; direct checkout when the tier is clear; the full comparison screen survives | 02, 03, 04, 08, 10; 04 keeps all three tiers |
| 4 | Studio opens with posting and recent work; earnings are secondary but reachable | 16 |
| 5 | Compact tier cards on the profile, with an obvious route to the full comparison | 03 |

No sixth decision was taken. Anything that would have required a new product decision
(social features, search, notifications) was left out rather than invented.

---

## Round A — surface model (`assets/app.css`)

- Added a **surface triad**: browsing ground `#FBF8F3`, card `#FFFDFA`, media ink `#141110`.
  Bone `#F4EFE7` is now scoped to chrome, lists and billing (`body.screen.bone`).
- **One card**: 14 px radius, 10% ink border, no shadow. Inside it a **cover** runs edge to
  edge; an **overlapping avatar** breaks the cover edge; three **chips** (counter, price,
  lock) sit on the image where the decision is made.
- **Action with price inside** (`.btn.cta`): 52 dp, price set in tabular mono at the trailing
  edge, so the amount and the action are one object.
- **Tier card** and **tier preview** replace the hairline membership list.
- **Grids**: 2-up and 3-up square cells with 4 dp and 3 dp gutters; locked cells blur and
  desaturate rather than wearing a padlock badge.
- **Action bar**: sticky, card-coloured, sits above the ground so the primary action never
  floats over a photograph.

## Round B — media-first screens

| Screen | Composition now | Photography share of the content area |
| --- | --- | --- |
| 02 Discover | Featured cover 236 dp, name and price, Join button carrying `$9.00 / month`, then a 2-up "New this week" grid | **86%** (was 20%) |
| 03 Creator | 176 dp cover, overlapping avatar, stats, two-line bio, tier preview, 6-cell work grid, sticky Join bar | **50%** |
| 07 Feed | Post cards: 296 dp image with counter chip, honest PPV preview carrying its price | **65%** |
| 11 Library | Owned media as a 220 dp hero cell plus a 2-up unlock grid, with type and dates | **79%** |

Share is measured on the 743 dp content area (915 dp frame minus system bar, chrome and
72 dp tab bar), counting image area only.

## Round C — tiers, studio, billing

- **04 Membership**: three stacked tier cards. Benefits are sentences, the price is the
  largest figure, the button repeats it, and only one tier is marked "most chosen".
- **16 Studio**: New post first, recent-posts grid with status chips second, earnings card
  third, audience figures below. The chart and payouts are reachable but no longer the
  opening statement.
- **18 Tiers & audience**: the same tier cards with member counts and per-tier revenue; the
  retirement rule is stated in words, matching D-036.
- **13 Subscriptions**: creator cards with the sold price, the renewal date and the
  price-lock line from D-036.

## Round D — alignment of the remaining screens

05 checkout, 06 payment states, 08 locked post, 10 unlock sheet and 17 composer were moved
onto the same triad: order summary as a card, state cards instead of a rules list, a
full-bleed blurred preview with the lock chip, a price block, and a composer that previews
the post as the subscriber will see it.

01 splash, 09 viewer, 12 account, 14 statement and 15 alerts are unchanged in structure and
only inherit the surface tokens. The viewer keeps its top-anchored chrome and 22% watermark;
the statement keeps its compact ledger.

## Round E — board

The board gained a **recomposition section**: what each screen was, what it is now, and the
measured photography share; plus the surface triad explained once. The category table now
names OnlyFans as the primary benchmark. Questions at the end were rewritten to ask what
still reads as a design object rather than re-asking settled decisions.

## Round F — verification

Rendering is the only step that can prove composition, and it did its job: the first
render exposed three real defects that the arithmetic had missed. They were fixed in two
correction passes, each measured from the PNG rather than guessed.

| Defect (measured on the render) | Cause | Fix |
| --- | --- | --- |
| 03 second grid row cut by 29 dp | cover plus copy taller than the content area | cover 200 → 176, two margins 12 → 8 |
| 06 fifth state card cut at the frame edge | five cards at 16 dp padding | four cards at 12 dp; the duplicate-webhook case moved into the paid card |
| 02/11/16 68/83/114 dp of dead space | content shorter than the frame | larger covers, a closing-access row in Library, a last-7-days strip in Studio |
| 13 304 dp of dead space on a list screen | only active subscriptions were shown | ended subscriptions, then a billing explainer |
| 08 177 dp gap above the sticky bar | cover too short for the copy | cover 392 → 510 and the delivery note |

After the corrections the bottom gap is 2–15 dp on 02, 03, 07, 11 and 16, and every card
edge is inside the frame. This is the honest cost of never rendering intermediate versions:
three renders of the same direction, each one paid for by a defect it revealed.

Static audit (`audit.py`, run in CI, report-only):

| Check | Result |
| --- | --- |
| Type sizes | **7** distinct (40 / 28 / 21 / 16 / 14 / 12 / 11); none ≤ 10.5 px |
| Spacing values | **7** distinct (2 / 4 / 8 / 12 / 16 / 20 / 24) |
| Tracking | **2** (0.02 UI, 0.10 labels) |
| Touch targets | icons 48 dp, compact buttons 44 dp, tab bar 72 dp |
| Typographic chevrons | **0** — every chevron is an SVG path |
| Figures | 12,412 members = 4,120 + 6,310 + 1,982; $115,048 MRR; Studio 50.8% |
| Screen density | thinnest content screen 65 words (splash remains 39 by design) |

Rendering is done by `.github/workflows/design-render-v3.yml` on GitHub-hosted runners with
Playwright. `board.png` is 1720 wide; each screen is captured at 824 × 1830. Three renders
were used for this direction: the first composition, then two measured corrections
(commits `f15d588`, `b328e99`, and the final one).

---

## What this deliberately does not do

- **No new features.** Every screen shows a route the backend already serves. Likes,
  comments, view counts, DMs, tips, search, notifications, trending, follower feeds, trials
  and bundles are absent because no backend exists for them.
- **No production change.** No Android source, route, schema, migration or dependency was
  modified. The app remains the Compose shell; the token set is still a placeholder.
- **No brand decision.** "Vesper" and the eclipse mark are placeholders until D-006 closes.
- **No licensed assets.** The four photographs are generated stand-ins for composition
  testing. They are not licensed imagery and must not ship.

## Risks the render will expose

1. **Text fitting.** Heights were computed, not measured: the profile grid and the feed's
   second card are the most likely to clip by a few dp.
2. **Crop behaviour.** Portraits are square sources placed in 4:5 and 1:1 frames; the
   `object-position` values are estimates and one may crop a face badly.
3. **Density.** Moving from rules to cards increases visual weight; if the ground reads
   heavy, the card border drops from 10% to 8% rather than the radius changing.
4. **Placeholder fatigue.** Four faces across eighteen screens still repeats; a real shoot
   needs at least twelve frames.

## The ask

Review the rendered board and the individual screens, then answer:

1. Does this read as a creator subscription product rather than a publication?
2. Is photography dominant enough on 02, 03, 07 and 11, and is the type quiet enough?
3. Is the price inside the button the right amount of selling?
4. Does Studio leading with posting match how a creator works?
5. What still feels like a design object rather than an app someone uses?

No production UI work starts until the images are approved.
