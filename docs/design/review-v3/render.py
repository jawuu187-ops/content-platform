#!/usr/bin/env python3
"""Render review-v3 (board + 18 screens) to PNG.

GitHub-hosted runners only; see .github/workflows/design-render-v3.yml.
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
BOARD_DARK_OUT = ROOT / "board-dark.png"

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


def render_screens(browser) -> None:
    OUT.mkdir(parents=True, exist_ok=True)
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
    context.close()


def render_board(browser, source: pathlib.Path, out: pathlib.Path) -> None:
    context = browser.new_context(
        viewport={"width": 1720, "height": 1200},
        device_scale_factor=1,
        reduced_motion="reduce",
    )
    page = context.new_page()
    page.goto(source.as_uri(), wait_until="load")

    deadline, elapsed, step = 40_000, 0, 250
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
        except Exception as exc:  # noqa: BLE001
            print(f"warning: frame settle failed: {exc}")

    # Chromium paints iframes lazily; a short viewport leaves them blank in a full-page capture.
    height = int(page.evaluate("() => document.body.scrollHeight"))
    page.set_viewport_size({"width": 1720, "height": min(height, 12000)})
    page.wait_for_timeout(600)
    y = 0
    while y < height:
        page.evaluate(f"window.scrollTo(0, {y})")
        page.wait_for_timeout(110)
        y += 500
    page.evaluate("window.scrollTo(0, 0)")
    page.wait_for_timeout(800)

    page.screenshot(path=str(out), full_page=True, animations="disabled")
    print(f"board    {out.relative_to(ROOT)}  {out.stat().st_size // 1024} KiB")
    context.close()


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--only", choices=("screens", "board", "board-dark"), default=None)
    args = parser.parse_args()

    with sync_playwright() as pw:
        browser = pw.chromium.launch(args=["--font-render-hinting=none"])
        if args.only in (None, "screens"):
            render_screens(browser)
        if args.only in (None, "board"):
            render_board(browser, ROOT / "board.html", BOARD_OUT)
        if args.only in (None, "board-dark"):
            render_board(browser, ROOT / "board-dark.html", BOARD_DARK_OUT)
        browser.close()


if __name__ == "__main__":
    main()
