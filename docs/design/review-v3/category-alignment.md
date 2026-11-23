# Aligning direction 03 with the creator-subscription category

**Proposal only.** No files under `screens/` or `assets/` changed, nothing rendered, no
production code, no brand decision. This document compares the approved v3 screens with the
experience conventions of the category leader and proposes the specific compositional
changes that would close the gap — screen by screen — while keeping our identity.

---

## 1. The correction in one paragraph

v3 won on craft and lost on category. It reads as a *printed catalogue about* a
subscription platform: a date line and a headline open the home screen, creator names are
set in didone above hairline index rows, and photography is a single framed moment inside a
typographic page. A mature creator platform is the opposite hierarchy: **photography is the
page, the interface is the caption.** The work below does not abandon v3's type system,
spacing discipline, component quality or identity. It changes the *composition model* from
"editorial index" to "media-first product", and it pushes the didone, the rules and the mono
labels back to the moments where they earn their place.

---

## 2. Measured evidence from the current board

| Signal | v3 today | Category norm | Source |
| --- | --- | --- | --- |
| Photography share of the first screenful (02 Home) | ~20% (one 200 dp feature) | 50–65% | render of `02-discover` |
| Screens where a photo avatar is a real image | **1 of 18** (viewer only) | every list, feed and profile | `grep 'class="avatar'` |
| Monogram plates standing in for people | **16 occurrences** | fallback only | `grep 'class="plate'` |
| Primary buttons that state the price | **3 of 18** | the subscribe action always carries price | 05, 08, 10 |
| Screens using a 3-up media grid | **1 of 18** | profile and library are grids | 03 |
| Screens built on hairline ruled lists | **7 of 18** | lists only in settings/billing | ruled-row count |
| Home screen opening element | date line + "Tonight." headline | a creator or a piece of content | 02 |
| Creator home (16 Studio) opening element | available balance and a chart | new post + recent content + earnings | 16 |

The diagnosis is consistent across screens: **the rule is the primary separator, the card is
absent, the person is a monogram, and the photograph is an illustration of the layout rather
than the subject of the screen.**

---

## 3. What the category actually does (structure, not branding)

Ten conventions worth adopting. These are structural; none of them require copying a logo,
palette or literal screen.

1. **Home is a feed of people, not an index.** Creator cards carry a portrait, an avatar, a
   name, a short line and a price; the scroll is content, not navigation.
2. **Cover + overlapping avatar** on a profile: a wide cover image, a circular avatar
   breaking its lower edge, name and verification beside it.
3. **The subscribe action is a persistent bar with the price inside the button** — never a
   separate step to discover the price.
4. **Media is presented in grids and cards with rounded corners**, not as ruled table rows;
   paid cells carry a lock and a price chip on the image itself.
5. **Posts are cards**: author row, media, caption, one action row, generous vertical gap.
6. **Tiers are comparable cards** with benefit bullets and a Select button each, one tier
   marked as recommended.
7. **Locked content is a professional preview**: a dimmed or cropped frame, a lock chip, the
   price, and one obvious action.
8. **The viewer is immersive**: dark, chrome reduced to a top bar and a scrubber, subject
   untouched.
9. **The creator area leads with making content**, then audience, then money — the inverse
   of our current studio screen.
10. **Density is uneven on purpose**: browsing screens are image-heavy and sparse; billing
    and statements are dense and tabular. One density rule for everything is what makes a
    product feel unprofessional.

### What we must not adopt, because the backend cannot support it

Likes and comment counts, direct messages, tips, search, notifications, trending, views,
followers feed, mass messaging, free trials, bundles. v3 already refuses these honestly;
this proposal keeps refusing them. Where the category shows an engagement number and we have
none, we show **member count, post count, unlock count or nothing** — never a fabricated
number.

---

## 4. How the identity survives

The identity does not live in the paper background or in the rules. It lives in five things,
all of which stay:

| Anchor | Where it stays | Change |
| --- | --- | --- |
| Eclipse mark + VESPER wordmark | splash, home masthead, share cards | unchanged |
| Oxblood `#7A1F2B` as the single action colour | subscribe, unlock, primary CTA | now mostly *inside* buttons and price chips, not in rules |
| Didone (Bodoni Moda) | creator names, the price at the moment of decision, statement totals | demoted from section headlines and body copy |
| IBM Plex Mono, tabular | money columns, ids, ledger, timestamps | stops being used for every label |
| Warm bone base `#F4EFE7` | browsing chrome and page ground | becomes the *frame* around photography, not the subject |

**The revised surface triad** answers the "too much paper" concern:

- **Paper** `#F4EFE7` — chrome, lists, billing.
- **Card** `#FBF7F1` with a 14 dp radius and a 1 px 10% border — media, tiers, posts.
- **Media ink** `#141110` — full-bleed image blocks and the viewer, so photographs carry the
  screen instead of floating in beige.

Rules survive only where they belong: settings rows, statements, ledger tables. Everything
else separates by card, gap and image edge.

---

## 5. Screen-by-screen: current composition → target composition

Each row states what changes, what is kept, and what the backend can actually serve.

| # | Screen | Today | Target (category convention) | Kept from v3 | Backend |
| --- | --- | --- | --- | --- | --- |
| 01 | Splash | mark + "The work and the witness." | keep; add photography only if a licensed asset exists | mark, didone line, legal floor | none |
| 02 | Discover / Home | date line, headline, one feature card, index list | media-first: 1 large creator card (4:5 cover, avatar, price chip), then a 2-column photo grid of creators with price, then "new this week" row; headline and date removed | wordmark, panel rhythm, mono price, 48 dp targets | `GET /v1/creators?published` serves the list |
| 03 | Creator profile | 268 dp cover, monogram, 2 tier rows, 3 thumbs | 240 dp cover, **circular avatar overlapping 20 dp**, name + verified + stats, tier card strip, **sticky CTA with price in the button**, 3-up grid with lock badges | didone name, stats row, price adjacency | profile, tiers, posts, access all served |
| 04 | Membership | ruled price table | three tier **cards** with benefit bullets, recommended badge, Select per card, terms line | typographic benefit list, price hierarchy | tiers + perks served |
| 05 | Checkout | ledger table | card order summary with cover thumb, method row, fee shown as included, CTA with total | fee transparency, sandbox honesty | intents + ledger; sandbox only |
| 06 | Payment states | four text blocks | same states, restyled as cards with a status chip and one action | state vocabulary, retry semantics | intent status served |
| 07 | Feed | mixed rows and one full-bleed post | **post cards**: author row with avatar, media 14 dp radius with counter and price chip, caption 2 lines, action row (open/save/more) | media treatment, gallery counter, watermark line | posts, PPV, unlock, delivery served |
| 08 | Locked post | dimmed frame + copy | preview frame with lock chip, price, CTA bar; state what a tier includes | preview honesty, tier wording | access decided server-side |
| 09 | Viewer | dark, top chrome, three watermarks | unchanged except avatar in footer and slightly larger progress | all of it | single-use delivery |
| 10 | Unlock sheet | ledger + three flags | price block, what you get, one CTA, refund rule | price clarity | unlock + refund served |
| 11 | Library | text rows with thumbs | **2-column media grid** of owned items, unlock date and type chip; rows only for the empty state | unlock labels, honest "not available yet" | unlocks/entitlements served |
| 12 | Account | ruled settings list | keep the list (correct here); add avatar header | hairlines, mono metadata | identity, MFA, passkeys, sessions served |
| 13 | Subscriptions | rows with price lock | cards per creator with avatar, price lock, renewal date, cancel | price-lock copy (D-036) | served |
| 14 | Statement | ledger + receipt | keep (correct here); add cover thumb to the receipt block | density, ledger split | served |
| 15 | Alerts | list with states | keep as a list, unstyled further | empty/skeleton | no backend yet |
| 16 | Studio | balance first, chart, ledger | **content-first**: New post + recent posts grid with status; audience counts; earnings compact card; chart below | numbers, tabular figures, states | posts, members, ledger served; engagement not |
| 17 | Composer | media grid + fields | preview-first: large preview of the post as it will appear, then visibility and price | PPV pricing model | upload, visibility, price served |
| 18 | Tiers & audience | table with counts | tier cards with member counts and a manage action | counts, retire-not-delete wording | served |

### Journey changes (fan)

- **Today:** home → profile → membership → checkout (4 taps, price discovered at step 3).
- **Target:** home → profile → **CTA with price** → checkout sheet (3 taps, price visible at
  step 2). Membership survives as the comparison surface for multi-tier creators.
- Locked post → unlock sheet → viewer looks and behaves the same; only the presentation of
  the sheet changes.

### Journey changes (creator)

- Studio opens with **New post** and the last posts in a grid, not with a balance.
- Earnings move to a compact card and a second section, so the screen belongs to the same
  product as the fan side.
- Tier manager becomes cards with member counts, consistent with the fan-facing membership
  cards.

---

## 6. What changes, concretely, and what does not

**Changes**

- Composition model: from rules-and-columns to cards-and-images.
- Photography share: from ~20% to ≥50% of the first screenful on 02, 03, 07, 11.
- One circular photo avatar per creator wherever a person is listed.
- Radii: 14 dp on media and cards (0 on full-bleed heroes), buttons 2 dp → 8 dp.
- Price inside the primary action, always.
- Didone demoted to names and decision prices; Archivo takes headings.
- Rules reduced to settings, statements, ledger only.

**Does not change**

- Type scale (7 sizes), spacing grid (8 dp), 48 dp targets, focus/pressed/disabled specs.
- The ink system, the eclipse mark, the wordmark, oxblood as the only action colour.
- The states section (loading/empty/failure/stale/offline).
- The honesty rules: no fabricated metrics, no unsupported capability in copy.
- The data set: 12,412 members, $115,048 MRR, Studio 50.8%.

---

## 7. Risks and what we still reject from the benchmark

- **We reject its clutter**: uneven icon weights, banner-like promotion blocks, and the
  dated gradient chrome. We take its *structure*, not its craft level.
- **We reject invented social proof**: no likes, comments or view counts until a backend
  exists to produce them.
- **Risk — losing distinctiveness.** Mitigation: the identity stays in the mark, the single
  oxblood accent, the didone nameplate and the tabular money. If a screen would read the
  same with our logo removed, it has drifted too far and gets pulled back.
- **Risk — card overload.** Mitigation: cards only for media and tiers; everything else is
  space, not containers.

---

## 8. Proposed execution (on approval, one render at the end)

| Round | Scope | Touches |
| --- | --- | --- |
| A | Surface triad, card component, radii, avatar component, CTA-with-price | `assets/app.css` |
| B | Recompose 02, 03, 07, 11 (media-first) | 4 screens |
| C | Recompose 04, 13, 18 (tier cards) and 16 (content-first studio) | 4 screens |
| D | Align 05, 06, 08, 10, 17 to the card model; keep 01, 09, 12, 14, 15 as they are | 5 screens |
| E | Board: new comparison section, refreshed contact sheet | `board.html` |
| F | One render on GitHub-hosted runners; audit must stay at 7/2/8 with 0 glyph icons | CI |

Estimated change surface: one stylesheet, 13 screens, one board file. No new screens, no new
features, no backend change.

---

## 9. Decisions I need from you

1. **Photography share:** commit to media-first composition (≥50% of the first screenful on
   home, profile, feed, library), accepting that this reduces the typographic calm that v3
   was praised for in those four screens?
2. **Paper:** keep bone as the frame around photography, or move the browsing ground to a
   warmer near-white so images sit more like a product and less like a printed page?
3. **Subscribe action:** price inside the CTA with checkout as a sheet (3 taps total), or the
   current separate membership screen as the mandatory step (4 taps)?
4. **Studio:** lead with posting and recent content (category norm), with earnings below?
5. **Tiers:** cards on the profile strip, or keep the single comparison screen and only a
   summary line on the profile?

No implementation begins until you answer and approve the direction.
