# Dark theme and motion

Implementation note for the second refinement stage: full light **and** dark support, and a
motion vocabulary that is shared by every screen. Companion render:
[`board-dark.png`](board-dark.png) — all eighteen screens in dark, the palette in both
themes, and the motion rules.

Everything below is code in `android/app/src/main/java/com/contentplatform/app/ui/`. No
backend file, migration or API contract was touched, and no prior design version was
modified.

## 1. Theme architecture

| Layer | File | What it owns |
| --- | --- | --- |
| Palette | `theme/VesperPalette.kt` | Nineteen named roles per theme, plus `contrastRatio()` / `relativeLuminance()` |
| Type | `theme/Type.kt` | Bodoni Moda, Archivo, IBM Plex Mono (bundled TTFs); seven sizes 40/28/21/16/14/12/11 |
| Shapes | `theme/Theme.kt` | 6 / 8 / 14 / 20 / 28 dp; the 14 dp card and 8 dp button are the load-bearing two |
| Mapping | `theme/Theme.kt` | `VesperPalette → ColorScheme`, so Material components inherit the theme |
| Preference | `theme/ThemeController.kt` | `System` / `Light` / `Dark`, stored in SharedPreferences, read synchronously before the first frame |

Rules that make the system hold for future screens:

- **No raw hex outside `VesperPalette.kt`.** Screens read `LocalVesperPalette.current`; a new
  screen cannot accidentally ship light-only colours.
- **No `isDark` branches in screen code.** Dark is a value swap. The only `isDark` reads are
  inside the palette itself (media gradient, monogram) and the `ColorScheme` mapping.
- **Semantic roles, not colour names.** `action` is a fill, `actionText` is type, so the dark
  theme can split oxblood into two tones without any screen knowing.
- **Switching is a recomposition, not a restart.** `VesperTheme` sits above `PlatformApp`, so
  changing mode recomposes in place: navigation position, scroll offsets and an open sheet
  survive. There is no activity recreation and no flicker.

System mode resolves with `isSystemInDarkTheme()`, which also follows the device while the app
is open.

## 2. The dark palette

Dark is a warm night room, not an inversion: the ground keeps a brown undertone, ink is bone
rather than white, and oxblood splits into a deeper fill and a lifted text tone because 11 px
type at 4.5:1 cannot be met with one value on dark.

| Role | Light | Dark |
| --- | --- | --- |
| ground | `#FBF8F3` | `#12100F` |
| chrome | `#F4EFE7` | `#0A0908` |
| surface (card) | `#FFFDFA` | `#221E1C` |
| surfaceRaised | `#FFFFFF` | `#2A2522` |
| media | `#141110` | `#0A0807` |
| ink | `#191412` | `#F3EBE1` |
| inkSecondary | `#3C322C` | `#D9CDC2` |
| muted | `#6F635A` | `#A2938A` |
| action (fill) | `#7A1F2B` | `#8E2A37` |
| actionText | `#7A1F2B` | `#D06B76` |
| onAction | `#FFFDFA` | `#F6EFE6` |
| sage / brass / danger | `#2F6B4F` / `#8C6A2A` / `#A8322A` | `#6FBF95` / `#D3A85C` / `#E2705F` |

Measured contrast (WCAG 2.1):

| Pair | Light | Dark |
| --- | --- | --- |
| ink on ground | 17.24 | 16.07 |
| ink on card | 17.98 | 14.00 |
| secondary on ground | 11.77 | 12.16 |
| muted on ground | 5.49 | 6.39 |
| muted on card | 5.30 | 5.57 |
| actionText on ground | 9.63 | 5.46 |
| onAction on action | 10.05 | 7.26 |
| sage / brass / danger on ground | 5.94 / 4.71 / 6.28 | 8.64 / 8.61 / 6.07 |

`VesperPaletteTest` asserts these as thresholds rather than trusting the table: body text
≥ 7:1 on ground, card and chrome; secondary ≥ 7:1; muted ≥ 4.5:1 on all three surfaces;
status accents ≥ 3:1; and it refuses a dark theme that is pure black, pure white ink, or an
inversion of light (ground luminance above zero, card measurably separated from ground).

## 3. Motion

One vocabulary, three durations, one curve, one switch.

| Pattern | Duration | Where | Reduced motion |
| --- | --- | --- | --- |
| Feedback | 140 ms | press scale 0.97, tab tint, theme selection dot, chip state | scale stays 1, colour snaps |
| Transition | 220 ms | destination change (slide 1/12 width + fade), creator profile (rise 1/10 height), checkout sheet (rise from bottom, scrim fade), confirmation card | instant swap |
| Reveal | 320 ms | first composition of lists and cards, fade 0.35 → 1 plus 8 dp lift, staggered 40 ms capped at 240 ms | content composes at its final state |

Deliberate omissions, and the reason:

- **No animation on money, prices or contract copy** — movement on a number the user is
  agreeing to is a distraction and an accessibility hazard.
- **No scroll parallax, image zoom or sticky transforms** — battery and frame-time cost on
  mid-range devices for no information gain.
- **No looping shimmer** — skeletons hold their geometry; a perpetual animation is the
  fastest way to drain a battery in a feed.
- **No shake on declined payment** — mocking the user at the worst moment.
- **Everything is non-blocking.** Every animation is applied to a surface that is already
  interactive, and no duration exceeds 320 ms.

Implementation: `motion/Motion.kt`.

- `VesperMotion(reduced)` — `duration(base)` returns 0 when reduced, so a screen that asks for
  `motion.transition` automatically obeys the preference.
- `resolveReducedMotion(animatorScale, transitionScale)` — either Android scale at 0 means
  reduced; read once per composition from `Settings.Global`.
- `Modifier.pressScale(motion, onClick)` — shared press feedback with the ripple suppressed so
  the scale is not doubled by a Material indication.
- `Reveal(index, motion) { alpha, offsetDp -> }` — the single reveal primitive. It starts at
  alpha 0.35, so a zero-duration run is indistinguishable from a static layout rather than a
  pop-in.
- `rememberScrimAlpha(visible)` — one scrim fade for every overlay.

Motion is a local (`LocalVesperMotion`), so a screen cannot invent its own timings without
deliberately bypassing the system.

## 4. Screens in this stage

`ui/PlatformApp.kt` — four destinations (Home, Tiers, Studio, You) with an `AnimatedContent`
switch, plus two overlays: the pushed creator profile and the checkout sheet, with the
confirmation card on the same layer as the sheet it replaces. Bottom bar keeps the design's
72 dp footprint and marks each destination with a shape rather than an icon font.

`ui/screens/Screens.kt` — Home, Creator, Membership, Studio, Account (with the Appearance
card: System / Light / Dark), Checkout sheet, Payment result. All figures come from the
canonical dataset already used by the board (12,412 members, $115,048 MRR, 50.8% on Studio,
37 unlocks in 7 days, $1,284.40 available); nothing invents a metric the backend does not serve.

`ui/components/VesperComponents.kt` — chrome bar, chip (five tones), action button with the
price inside, outline button, 14 dp card, media panel, monogram, money, mono label, divider,
section title, scrim.

Fonts are bundled at `res/font` (6 TTFs, ~570 KB) with the SIL OFL text at
`assets/OFL.txt`: no network font provider, no first-frame flash, no runtime download.

## 5. What is verified, and how

Evidence, all on GitHub-hosted runners:

| Check | Where | Result |
| --- | --- | --- |
| Kotlin compile, unit tests, lint, debug APK, release AAB | `android-ci.yml`, run `45227154869` | success |
| Palette, motion and mode tests | same run, `:app:testDebugUnitTest` | success |
| Security scanners (gosec, staticcheck, govulncheck, gitleaks, Trivy) | `security.yml`, run `45227154822` | success |
| Dark board and all eighteen light screens rendered | `design-render-v3.yml`, run `45229078418` → `9dbf0aa` | success |
| Documentation links and structure | `docs.yml` | success |

Tests: `VesperPaletteTest` (contrast thresholds, dark-is-not-an-inversion rules),
`MotionTest` (reduced-motion resolution, zeroed durations, stagger cap),
`ThemeModeTest` (System default, round trip, corrupt value fallback).

There is no emulator in the pipeline, so no screenshot of the running app exists; the dark
screens in this folder are HTML renders of the approved compositions, driven by the same
palette values the Kotlin theme holds. Motion was reviewed as a specification plus code, not
as a recording — a frame-accurate capture needs a device or emulator runner.

Dark-mode review of the render found five real defects, all fixed before this note:
a cream tab bar and sheets (the light theme used one token for both page substrate and
on-fill type), chips whose type became bone-on-dark, a cream "Open the feed" button,
a cream ink-plate avatar, and two frames whose content ran under the tab bar (04, 12).
The light compositions are unchanged except for those last two clipping fixes.

## 6. Still open

1. **Photography** — the board's images are generated stand-ins and remain unlicensed; the
   app draws a graded placeholder in media ink instead, so nothing pretends to be content.
2. **Live data** — screens render the canonical dataset; wiring to the API is a separate step.
3. **Device verification** — dark mode and motion need one pass on real mid-range hardware to
   confirm frame times and the reduced-motion switch on OEM builds; that needs a device or an
   emulator runner, neither of which exists in this pipeline today.
4. **Tab icon gap** — the 3 dp misplacement noted in the previous review is still open.
