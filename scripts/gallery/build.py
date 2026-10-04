#!/usr/bin/env python3
"""Build a click-through gallery of docs/screenshots (stdlib only).

Reads the table in docs/screenshots/README.md (# | Flow | File), groups the shots by
file name (test cases, test runs, errors, phone), and writes <out>/index.html plus
<out>/shots/*.png. The output is self-contained and can be published as an Artifact.

Usage: scripts/gallery/build.py --out DIR [--new 27,28] [--note "what changed"]
  --new   shot numbers to badge as New (flows added or fixed in this round)
  --note  header paragraph (date, what was validated)
Exit 1 when the README lists a file that does not exist.
"""
import argparse, html, json, re, shutil, sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
SHOTS = ROOT / "docs" / "screenshots"
GROUPS = ["Test cases", "Test runs", "Errors", "Phone (375 px)", "Other"]
ROW = re.compile(r"^\|\s*(\d+)\s*\|\s*(.+?)\s*\|\s*`([^`]+\.png)`\s*\|$")


def group(file):
    if "mobile" in file:
        return "Phone (375 px)"
    if "not-found" in file or "invalid-id" in file or "validation-error" in file:
        return "Errors"
    if "test-run" in file:
        return "Test runs"
    if "test-case" in file:
        return "Test cases"
    return "Other"


def inline_md(text):
    # The README uses `code` spans; everything else is plain text.
    return re.sub(r"`([^`]+)`", r"<code>\1</code>", html.escape(text, quote=False))


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--out", required=True)
    ap.add_argument("--new", default="")
    ap.add_argument("--note", default="Every screen captured by make screenshots (real Chromium, API and Postgres). Desktop at 1440 px, phone at 375 px.")
    args = ap.parse_args()
    new = {n.strip().zfill(2) for n in args.new.split(",") if n.strip()}

    rows = [ROW.match(l.strip()) for l in (SHOTS / "README.md").read_text().splitlines()]
    rows = [m.groups() for m in rows if m]
    missing = [f for _, _, f in rows if not (SHOTS / f).is_file()]
    if missing:
        sys.exit("missing screenshots: " + ", ".join(missing))

    by_group = {g: [] for g in GROUPS}
    for num, flow, file in rows:
        stem = file[:-4]
        by_group[group(file)].append([stem, inline_md(flow), "", num.zfill(2) in new, "mobile" in file])
    flows = [[g, shots] for g, shots in by_group.items() if shots]

    out = Path(args.out)
    (out / "shots").mkdir(parents=True, exist_ok=True)
    for _, _, file in rows:
        shutil.copy2(SHOTS / file, out / "shots" / file)
    page = (Path(__file__).parent / "template.html").read_text()
    page = page.replace("__NOTE__", html.escape(args.note)).replace("__FLOWS__", json.dumps(flows, ensure_ascii=False, indent=1))
    (out / "index.html").write_text(page)
    print(f"{len(rows)} shots in {len(flows)} groups -> {out / 'index.html'}")


if __name__ == "__main__":
    main()
