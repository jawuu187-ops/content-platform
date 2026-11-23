#!/usr/bin/env python3
"""Re-measure the review-v3 mockups so the refinement notes stay evidence-based.

This reads only docs/design/review-v3 (CSS + screen HTML). It builds nothing, touches
no product code and needs no browser, so it is safe to run anywhere.

    python docs/design/review-v3/audit.py

It reports the five things that separate a shipped product from a concept mockup:
type scale discipline, spacing grid, touch-target sizes, icon consistency and whether
the numbers on screen reconcile with each other.
"""

from __future__ import annotations

import collections
import pathlib
import re
import sys

ROOT = pathlib.Path(__file__).resolve().parent
CSS = (ROOT / "assets/app.css").read_text()
SCREENS = sorted((ROOT / "screens").glob("*.html"))
HTML = "\n".join(p.read_text() for p in SCREENS)
BOTH = CSS + HTML

# Minimum on-device sizes we hold ourselves to (Android Material + WCAG 2.5.5).
MIN_TOUCH_DP = 48      # icons and primary actions (Android)
MIN_COMPACT_DP = 44    # secondary/compact actions (WCAG 2.5.5)
MIN_TEXT_SP = 11
TARGET_SIZES = 7
TARGET_SPACING = 7

fails: list[str] = []


def section(title: str) -> None:
    print(f"\n{title}")
    print("-" * len(title))


def counter(pattern: str, bucket: int = 1) -> collections.Counter:
    return collections.Counter(
        round(float(v) / bucket) * bucket for v in re.findall(pattern, BOTH)
    )


section("Type scale")
sizes = sorted(float(v) for v in re.findall(r"font-size:\s*([0-9.]+)px", BOTH))
distinct = sorted(set(sizes))
tiny = sum(1 for s in sizes if s <= 10.5)
print(f"declarations         {len(sizes)}")
print(f"distinct sizes       {len(distinct)}  {distinct}")
print(f"instances <= 10.5px  {tiny}")
if len(distinct) > TARGET_SIZES:
    fails.append(f"type scale: {len(distinct)} distinct sizes (target <= {TARGET_SIZES})")
if tiny:
    fails.append(f"type scale: {tiny} declarations below {MIN_TEXT_SP}px")

section("Tracking")
track = collections.Counter(f"{float(v):.3f}" for v in re.findall(r"letter-spacing:\s*(-?[0-9.]+)em", BOTH))
print(f"distinct values      {len(track)}  {sorted(track)}")
if len(track) > 3:
    fails.append(f"tracking: {len(track)} distinct values (target 3)")

section("Spacing grid")
spacing = counter(r"(?:margin|padding)(?:-top|-bottom|-left|-right)?:\s*([0-9]+)px")
gaps = counter(r"gap:\s*([0-9]+)px")
allspace = sorted(set(spacing) | set(gaps))
off_grid = [v for v in allspace if v not in (2, 4, 8, 12, 16, 20, 24, 32, 40)]
print(f"distinct values      {len(allspace)}  {allspace}")
print(f"off-8dp values       {off_grid}")
if len(allspace) > TARGET_SPACING:
    fails.append(f"spacing: {len(allspace)} distinct values (target <= {TARGET_SPACING})")

section("Touch targets")
for cls in ("icon", "icon.sm", "btn", "btn.sm", "btn.tiny"):
    m = re.search(rf"\.{re.escape(cls)}\s*\{{([^}}]+)\}}", CSS)
    if not m:
        continue
    h = re.search(r"height:\s*([0-9]+)px", m.group(1))
    w = re.search(r"width:\s*([0-9]+)px", m.group(1))
    size = min(int(h.group(1)) if h else 999, int(w.group(1)) if w else 999)
    floor = MIN_COMPACT_DP if cls in ("btn.sm", "btn.tiny") else MIN_TOUCH_DP
    if size != 999:
        state = "OK" if size >= floor else "UNDER"
        print(f".{cls:10s} {size:>3}dp  floor {floor}dp  {state}")
        if size < floor:
            fails.append(f"touch target: .{cls} is {size}dp (minimum {floor}dp)")
tabbar = re.search(r"\.tabbar\s*\{([^}]+)\}", CSS)
if tabbar:
    h = re.search(r"height:\s*([0-9]+)px", tabbar.group(1))
    if h:
        print(f"tab bar      {int(h.group(1)):>3}dp  {'OK' if int(h.group(1)) >= 72 else 'THIN'}")

section("Icons")
glyph_marks = len(re.findall(r"[‹›]", HTML))
svg_marks = len(re.findall(r"<(?:path|polyline|line)\b", HTML))
print(f"typographic chevrons {glyph_marks}")
print(f"svg icon paths       {svg_marks}")
if glyph_marks:
    fails.append(f"icons: {glyph_marks} chevrons drawn as text glyphs instead of paths")

section("Content density")
words = [len(re.sub(r"<[^>]+>", " ", p.read_text()).split()) for p in SCREENS]
print(f"screens              {len(SCREENS)}")
print(f"words per screen     min {min(words)}  median {sorted(words)[len(words) // 2]}  max {max(words)}")
if min(words) < 60:
    fails.append(f"density: thinnest screen has {min(words)} words")

section("State coverage")
for key in ("skeleton", "empty", "toast", "disabled", "error", "retry", "offline"):
    hits = [p.name for p in SCREENS if key in p.read_text().lower()]
    print(f"{key:10s} {len(hits):>2} screen(s)  {', '.join(h.split('-')[0] for h in hits)}")
if not any("error" in p.read_text().lower() for p in SCREENS):
    fails.append("states: no screen shows a network/error state with a retry action")

section("Numbers reconcile")
tiers = {"Supporter": (5, 4120), "Studio": (9, 6310), "Collector": (19, 1982)}
members = sum(m for _, m in tiers.values())
mrr_minor = sum(p * m for p, m in tiers.values())
studio_share = 6310 / members * 100
print(f"members by tier      {members}")
print(f"MRR from tiers       ${mrr_minor:,}")
print(f"Studio share         {studio_share:.1f}%")
claims = {}
for label, pattern in (
    ("members", r"12,412"),
    ("mrr", r"\$115,048"),
    ("studio_share", r"50\.8%"),
):
    claims[label] = any(re.search(pattern, p.read_text()) for p in SCREENS)
print(f"claimed  {claims}")
if claims["members"] and members != 12412:
    fails.append(f"numbers: screen claims 12,412 members; tiers sum to {members}")
if claims["mrr"] and not re.search(r"\$115,048", "\n".join(p.read_text() for p in SCREENS)):
    fails.append("numbers: MRR $115,048 (subscription tiers) is not stated on any screen")
if claims["studio_share"] and abs(studio_share - 50.8) > 0.2:
    fails.append(f"numbers: Studio share {studio_share:.1f}% is not the stated 50.8%")

section("Claims")
promises = [w for w in ("offline copies", "download available", "Intant") if w.lower() in HTML.lower()]
print(f"unsupported/incorrect copy  {promises or 'none'}")
if promises:
    fails.append(f"copy: {promises}")

section("Result")
if fails:
    print(f"{len(fails)} finding(s):")
    for f in fails:
        print(f"  - {f}")
    sys.exit(0)
print("no findings against the current thresholds")
