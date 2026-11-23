# Direction 03 — refinement proposal

**Status: proposal only. Nothing implemented, no render regenerated, no brand finalised.**
Evidence was re-measured with `docs/design/review-v3/audit.py` against the committed
mockups (`8ea7511`). Read this against the screens themselves: `board.png` for the system,
`png/01…18-*.png` for the individual failures.

---

## 1. Verdict

v3 earned its direction. The ink system, the didone/grotesk/mono split, the ruled
(not card-based) structure, square media and price-as-type are the reasons it reads as
art-directed rather than generated, and none of that should change.

What is still missing is **finish**, and finish is measurable. v3 looks like a very good
*design document*. A shipped product differs in four ways we have not yet paid for:

1. **A restrained scale.** Ours has 35 type sizes, 12 tracking values and 20 spacing
   values. Production systems have seven, three and eight. Irregularity at that volume is
   the single loudest "mockup" signal, and it is invisible in a rationale.
2. **Data that reconciles.** Screen 16 claims 12,412 members and $93,140 MRR. The tier
   rows on the same board sum to 12,824 members and $116,284. A product never ships that.
3. **States and targets.** 34 dp icon buttons, 19 dp flags, 8 px tab labels, one failure
   state in 18 screens.
4. **Photography with range.** One subject, sixteen arbitrary crops, no landscape, no
   video affordance.

So: **direction approved-quality, craft not yet.** Below is what to fix, screen by screen,
with an acceptance test for each so "done" is not a matter of opinion.

---

## 2. Measured audit (reproducible)

```
type scale     176 declarations · 35 distinct sizes · 37 instances <= 10.5px
tracking       12 distinct letter-spacing values
spacing        20 distinct px values; off-grid: 1,3,5,6,7,9,10,11,13,14,18,22,26
targets        .icon 34dp  .icon.sm 28dp  .flag 19dp  .btn 46dp  .btn.sm 36dp  .btn.tiny 30dp
               tab bar 64dp (Android with labels is 80dp; iOS 83pt)
icons          21 chevrons drawn as text glyphs ‹ › · 42 svg paths
density        18 screens · words per screen 39 / 108 / 134 (min/median/max)
states         skeleton 1 screen · empty 1 · failure 1 (declined payment) · disabled 2
               network error, stale data, offline read: 0
numbers        claims 12,412 members / $93,140 MRR / 71% Studio
               tiers compute 12,824 members / $116,284 MRR / 49.2% Studio
```

Run it yourself: `python3 docs/design/review-v3/audit.py`.

---

## 3. Where it still falls short — ten findings

Each is a place a mature product differs from a concept deck. Ordered by how much it
damages credibility.

### F1 · The type scale is not a system
35 sizes means every screen was typeset by eye. 8 px tab labels, 9 px flags, 9.5 px
section labels, 12.5 px body: the app gets quieter as it gets smaller instead of holding
a readable floor. Real platforms keep supporting text at 11–12 and let hierarchy come
from weight and colour.

**Fix:** seven sizes — 40 display / 28 / 21 / 16 / 14 / 12 / 11. Three trackings —
`0.02` UI, `0.10` labels, `0.16` stamps. Nothing below 11.
**Acceptance:** `audit.py` reports `distinct sizes 7`, `instances <= 10.5px 0`.

### F2 · Spacing is hand-placed, not gridded
20 values, thirteen of them off an 8 dp grid. It shows as uneven rhythm between the
masthead, section heads and lists — the eye reads 14/16/18/20/22 as "almost aligned",
which is worse than clearly different.

**Fix:** 4 / 8 / 12 / 16 / 24 / 32 / 40 only; one page margin (22 → 20); section spacing
becomes a token pair (`32 above, 12 below`).
**Acceptance:** `off-8dp values` is empty.

### F3 · Touch targets below the platform minimum
34 dp icons, 28 dp small icons, 30 dp tiny buttons, 19 dp flags, 64 dp tab bar. A shipped
Android app cannot ship these; they are the first thing a real device exposes.

**Fix:** visual size may stay, hit areas become 48 dp (padding, not growth); flags to
24 dp with 11 px text; tab bar to 80 dp with 11 px labels and 2 px indicator.
**Acceptance:** every interactive class ≥ 48 dp; tab bar ≥ 72 dp.

### F4 · Two icon languages
Chevrons are text glyphs `‹ ›` (21 uses) while other icons are 1.5 px SVG paths (42).
Glyph chevrons inherit font metrics, sit optically low, and change weight between
typefaces — the classic tell of a hand-built mockup.

**Fix:** one 1.5 px stroke SVG set at 20/24 dp: chevron, close, more, heart, comment,
bookmark, play, lock, check, alert, share, download, filter, search, eye.
**Acceptance:** `typographic chevrons 0`.

### F5 · The numbers on screen do not reconcile
12,412 vs 12,824 members; $93,140 vs $116,284 MRR; "71% of members" vs 49.2% computed from
the tier rows. Plus `Intant access` (screen 10) and a chart with no scale, no axis and no
values.

**Fix:** one canonical seed dataset (tiers → members → MRR → unlocks → payouts) used by
every screen, and a non-zero axis with labelled max on the studio chart.
**Acceptance:** `audit.py` "Numbers reconcile" section passes; no spelling finding.

### F6 · Failure and staleness are almost unrepresented
18 screens contain one failure (a declined payment) and one skeleton. There is no network
error, no stale feed, no retry that is not payment-related, no "content removed" state.
A product is largely these screens.

**Fix:** a state matrix — for feed, library, statements, alerts, studio, profile: loading
(skeleton matching real geometry), empty (with a next action), error (retry + what
happened), stale (cached timestamp), offline (read-only). Nine new frames, not nine new
screens: they are states of existing screens.
**Acceptance:** each list screen has four documented states in the board.

### F7 · Content is thinner than the product it imitates
Median 108 words per screen; the profile carries 76 and three thumbnails; the feed has no
comment preview, no overflow menu, no gallery indicator on a 14-image post. Mature
platforms look dense because real density is *decisions already made* — overflow menus,
relative timestamps, "edited", "pinned", "expires in 6 days".

**Fix:** raise real density on 03, 07, 11, 14, 16 — comment previews, post overflow,
relative+absolute time, gallery counts, expiry, pinned, edited markers.
**Acceptance:** median ≥ 140 words with no invented structure the backend lacks; every
added affordance listed in the README's backend table.

### F8 · Photography has one note
One subject across sixteen placements; every crop centred; no landscape; no 16:9 cover
distinct from 4:5 feed; no avatars (monogram plates everywhere); video posts have no
duration chip, play glyph or scrubber. Meanwhile screen 11 *promises* offline copies that
do not exist.

**Fix:** a shoot plan (three subjects × cover 16:9, portrait 4:5, square 1:1, vertical
9:16, plus two avatar crops); video chrome (duration, play, progress); avatars become
photos with monograms as fallback; delete the offline-copy promise until it is built.
**Acceptance:** ≥ 3 subjects and 5 aspect ratios across the set; zero UI copy asserting an
unimplemented capability.

### F9 · Navigation vocabulary drifts
Screen 16 renames the third destination from "Create" to "Studio"; alerts live under You
while the platform has a notification surface; no unread badge exists anywhere except a
bare "3" whose meaning is unclear on 02.

**Fix:** one destination list for all 18 screens; Alerts either gets its own destination
(five tabs) or an explicit entry point with a badge in You — this is the pending review
question, not something to assume; badges get a single 16 dp spec with a max "9+".
**Acceptance:** identical tab labels across all tabbed screens; one badge component.

### F10 · Review annotations sit inside the product frame
Screen 05 shows "Sandbox" and "Dev · No processor is connected" *inside* the phone; screen
06 labels are review vocabulary, not product copy. It is honest, and it is why the frame
reads as a document.

**Fix:** move every status/annotation to the caption outside the frame; inside the frame
keep only what a customer would see (a sandbox banner is legitimate product copy, "Dev"
is not).
**Acceptance:** no internal qualifiers inside any device frame.

---

## 4. Screen-specific refinements

Fixes marked **S** are system-level (F1–F4) and apply everywhere; only screen-specific work
is listed.

| # | Screen | What falls short now | Change | Acceptance |
| --- | --- | --- | --- | --- |
| 01 | Splash | 39 words; no legal floor; no progress affordance | Add 18+/terms line and version; specify the 600 ms brand beat and what it waits on | Frame reads as an app launch, not a poster |
| 02 | Discover | "3" badge has no meaning; three cards; no filter state; monogram avatars | Six cards in two rows; filter chips with a selected state; photo avatars; badge becomes "3 new" | A 412-wide screen showing ≥ 6 creators and one active filter |
| 03 | Profile | 76 words; monogram instead of photo; 3 identical crops; tier count (2 shown) contradicts "12.4k subscribers"; bar overlaps grid | Real avatar; six-item grid with mixed aspect; tier summary "3 tiers from $5"; stats as ruled row with labels; grid clears the bar | No overlap; grid shows 6 items with ≥ 2 aspect ratios |
| 04 | Membership | Perks 2/3/3 lines; "MOST CHOSEN" arbitrary; nothing shows what a tier excludes | Three perks per tier, consistent syntax; "includes everything in Supporter"; renewal and cancellation terms in the table | Each row is the same height and parses in one pass |
| 05 | Checkout | "Platform fee · included $1.80" reads additive; dev copy in frame; no cancellation terms | Fee shown as "included in $9.00" with the creator share as secondary; cancel terms line; annotations outside the frame | Total, fee and share cannot be misread |
| 06 | Payment states | Four states, no 3-D Secure challenge, no "already subscribed", no network failure | Add challenge + already-member + network failure; differentiate retryable vs terminal | Six states, each with exactly one primary action |
| 07 | Feed | No overflow, no comment preview, no gallery count, time shown one way only | Post menu; one comment preview; "1 / 14" on the image; "2 h ago · 14:02" | Feed reads dense without new backend capabilities |
| 08 | Locked post | Tease layer conveys access but not terms | State which tier opens it, and that $19 is one-off and permanent | A viewer can answer "what do I get, for what, for how long" |
| 09 | Viewer | No frame counter, no progress, no gestures, watermark at the edge of legibility | Counter "3 / 14", scrubber for video, swipe hint on first view, watermark at ≥ 12% opacity in two positions, safe-area aware | Chrome survives a 9:16 crop |
| 10 | PPV sheet | Typo "Intant access"; no refund or expiry line | Fix copy; add "permanent access" and refund rule; state wallet availability honestly | Zero spelling findings |
| 11 | Library | Rows thin; promises offline copies that do not exist | Group by creator, show expiry, remove offline claim, add sort | No claim without a backend |
| 12 | Account | Status list only; no session/device management despite real backend support | Add sessions/devices with revoke, passkey detail, verification expiry and renewal state | Every row maps to a live endpoint |
| 13 | Subscriptions | Good summary; no locked-in price shown, no per-row renewal date | Show "price locked at signup" per row (D-036) and the next charge date per creator | Reflects price-snapshot behaviour |
| 14 | Statement | No period control, no refunded state, receipt download implied | Period selector; refunded row variant; "receipt" marked unavailable | Ledger split intact, states complete |
| 15 | Alerts | Under You; skeleton and empty only | Decide entry point with the owner; add grouped days, unread spec, badge | Reachable in one tap from any tabbed screen |
| 16 | Studio | Chart unverifiable; KPIs contradict tiers; "71%" wrong; payouts blocked with no path | Axis with max; MRR recomputed; payouts shows requirement and what to do next | Studio numbers equal the tier dataset |
| 17 | Composer | No upload progress, no validation, no audience preview | Progress per asset; inline validation; "visible to N subscribers"; scheduling marked not-enabled | A failed upload looks like a product, not a gap |
| 18 | Tiers & audience | No per-tier counts (backend lacks), no retire explanation | Show counts as "—" with "not tracked yet" rather than inventing; explain retire ≠ delete | No fabricated metric |

---

## 5. Fan ↔ creator consistency

The two halves share the ink system but not yet the furniture. Specifically:

- Creator screens lose the ruled-list language and drift toward dashboard cards (16)
  and form chrome (17), while fan screens are strictly typographic. Resolve toward the
  ruled list in both.
- There is no "preview as fan" affordance in 17/18, although the backend already
  distinguishes owner access from subscriber access (D-035/037). That is a real product
  feature worth showing and it is missing.
- Status vocabulary collides: "Subscribed", "Active", "Member", "Paid", "Verified 18+"
  all use sage. Assign one meaning per colour: sage = money settled; brass = pending;
  oxblood = access/ownership; red = failure.

---

## 6. What I would not change

The eclipse mark's construction · bone/oxblood/sage/brass roles · didone for names and
headline figures · grotesk for touch · mono tabular for comparable numbers · 1 px rules
as the separation device · square media · price adjacent to its action · four destinations
as a *proposal only*.

---

## 7. Execution plan (on approval, one render)

| Round | Work | Output |
| --- | --- | --- |
| R1 | Type scale, spacing grid, touch targets, icon set (F1–F4) | `app.css` + 18 screens re-inked |
| R2 | Canonical dataset, chart axis, arithmetic test (F5) | screens 02/03/04/16/18 consistent |
| R3 | State matrix, 9 new frames (F6) | board section "states" |
| R4 | Photography range + video chrome + remove unimplemented promises (F8) | 03/07/09/10/11 |
| R5 | Screen-specific table (§4), navigation vocabulary (F9), frames cleaned (F10) | all |

One render at the end of R5, on GitHub-hosted runners, replacing `board.png` and
`png/*.png`. R1–R5 touch only `docs/design/review-v3`.

---

## 8. Decisions I am not making

Not assumed, not implemented, waiting on you: visibility of the fee split to fans · product
name and final mark · licensed photography · whether Alerts becomes a fifth destination ·
payout schedule copy · whether creator handles/links appear on profiles.

No application code, schema, route, workflow or dependency was changed for this proposal.

---

## 10. Final correction pass (approved)

Applied to the same sources, no new concepts, no new screens:

1. **Viewer** — chrome anchored at the top again (it had been pushed to the bottom by the
   shared `.viewer .chrome` rule and was landing on the caption). The frame, the counter and
   the video controls are now laid out in flow: a flex column of status bar → chrome →
   image → footer. Controls can no longer sit on the subject. Watermark raised to 22% and
   placed at three fixed offsets.
2. **Creator profile** — the sticky action bar is a flex sibling of the content, not an
   absolute overlay, so it cannot cover the grid at any type scale. Inline tier prices moved
   from the didone to tabular figures (the didone dollar reads as broken strokes at 16 px).
3. **Statement** — headline reduced to the 28 step, compact ledger rows, tighter receipt
   block: the final row and both actions stay inside the screen.
4. **Chevrons** — the last four text glyphs in 02 and 05 replaced with the drawn icon.
5. **Targets** — `.icon.sm` to 48 dp (compact buttons stay at 44 dp, WCAG 2.5.5).
6. **Rhythm** — remaining 2/3/5/6 px values snapped to 4/8; display tracking set to 0 so
   only two tracking values exist in the whole system.
