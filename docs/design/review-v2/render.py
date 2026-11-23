#!/usr/bin/env python3
"""Render the review-v2 design board and every screen to PNG.

Runs only on a GitHub-hosted runner (see .github/workflows/design-render.yml).
Nothing here touches the application, the database or the backend.

    python render.py                # screens + board
    python render.py --only board   # board only
"""

from __future__ import annotations

import argparse
import pathlib
import sys

from playwright.sync_api import sync_playwright

ROOT = pathlib.Path(__file__).resolve().parent
SCREENS = ROOT / "screens"
OUT = ROOT / "png"
BOARD_OUT = ROOT / "board.png"

VIEWPORT_W, VIEWPORT_H = 412, 915
SCALE = 2

SETTLE_JS = """
async () => {
  await document.fonts.ready;
  await Promise.all(Array.from(document.images).map(img => img.decode().catch(() => {})));
  return true;
}
"""


def settle(page, extra_ms: int = 400) -> None:
    page.wait_for_load_state("networkidle")
    page.evaluate(SETTLE_JS)
    page.wait_for_timeout(extra_ms)


def render_screens(browser) -> list[pathlib.Path]:
    OUT.mkdir(parents=True, exist_ok=True)
    written: list[pathlib.Path] = []
    files = sorted(SCREENS.glob("*.html"))
    if not files:
        sys.exit("no screens found")

    context = browser.new_context(
        viewport={"width": VIEWPORT_W, "height": VIEWPORT_H},
        device_scale_factor=SCALE,
        reduced_motion="reduce",
    )
    page = context.new_page()
    for src in files:
        page.goto(src.as_uri(), wait_until="load")
        settle(page)
        target = OUT / f"{src.stem}.png"
        page.screenshot(path=str(target), full_page=False, animations="disabled")
        print(f"screens  {target.relative_to(ROOT)}  {target.stat().st_size // 1024} KiB")
        written.append(target)
    context.close()
    return written


def render_board(browser) -> pathlib.Path:
    board = ROOT / "board.html"
    context = browser.new_context(
        viewport={"width": 1720, "height": 1200},
        device_scale_factor=1,
        reduced_motion="reduce",
    )
    page = context.new_page()
    page.goto(board.as_uri(), wait_until="load")

    # Every phone is an iframe: wait for each frame to finish loading and settle.
    deadline = 40_000
    elapsed = 0
    step = 250
    while elapsed < deadline:
        ready = page.evaluate(
            "() => Array.from(document.querySelectorAll('iframe'))"
            ".every(f => f.contentDocument && f.contentDocument.readyState === 'complete')"
        )
        if ready:
            break
        page.wait_for_timeout(step)
        elapsed += step
    else:
        print("warning: some iframes did not report complete; rendering anyway")

    for frame in page.frames[1:]:
        try:
            frame.evaluate(SETTLE_JS)
        except Exception as exc:  # noqa: BLE001 - a frame may detach; render regardless
            print(f"warning: frame settle failed: {exc}")
    page.wait_for_timeout(1200)

    # Chromium paints iframes lazily. With full_page on a viewport shorter than the
    # document, every iframe below the fold comes out blank. Grow the viewport to the
    # document height and walk it instead, so each phone is painted before capture.
    height = int(page.evaluate("() => document.body.scrollHeight"))
    page.set_viewport_size({"width": 1720, "height": min(height, 9000)})
    page.wait_for_timeout(600)
    y = 0
    while y < height:
        page.evaluate(f"window.scrollTo(0, {y})")
        page.wait_for_timeout(120)
        y += 600
    page.evaluate("window.scrollTo(0, 0)")
    page.wait_for_timeout(800)

    page.screenshot(path=str(BOARD_OUT), full_page=True, animations="disabled")
    print(f"board    {BOARD_OUT.relative_to(ROOT)}  {BOARD_OUT.stat().st_size // 1024} KiB")
    context.close()
    return BOARD_OUT


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--only", choices=("screens", "board"), default=None)
    args = parser.parse_args()

    with sync_playwright() as pw:
        browser = pw.chromium.launch(args=["--font-render-hinting=none"])
        if args.only in (None, "screens"):
            render_screens(browser)
        if args.only in (None, "board"):
            render_board(browser)
        browser.close()


if __name__ == "__main__":
    main()
