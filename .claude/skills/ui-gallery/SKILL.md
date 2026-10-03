---
name: ui-gallery
description: Regenerate every UI screenshot and publish a click-through visual gallery of all Provenly flows as an Artifact. Use when Ed wants to "ver los flujos", "una captura de todos los flujos", a visual walkthrough before approving a card, or after UI changes.
---

# UI gallery

1. **Cover the flows.** New or changed screens need a capture in `e2e/screenshots/flows.spec.ts` (desktop 1440 px;
   phone 375 px with a no-horizontal-scroll assertion: `document.documentElement.scrollWidth <= innerWidth`) and a
   row in `docs/screenshots/README.md` (`| NN | Flow | NN-name.png |`, file name in backticks). Edit `e2e/` by hand (no Prettier there).
2. **Capture.** `make screenshots` (ephemeral database; needs Docker — `scripts/doctor.sh` if it fails).
3. **Look at them.** Open the new/changed PNGs and check layout, overflow, truncation, empty and error states.
   Anything broken is a finding: fix it (validate-card rules) and re-capture.
4. **Build.** `scripts/gallery/build.py --out <scratchpad>/gallery --new <shot numbers changed this round>
   --note "<date> — what was validated / changed>"`. It groups shots by flow and exits 1 if the README lists a
   missing file.
5. **Publish.** Artifact tool with `<scratchpad>/gallery/index.html` and every `shots/*.png` under `files`.
   Update the existing gallery (read it first, pass its `url`) instead of creating a new one; the current one is
   https://claude.ai/artifact/M56BhUBcQEAFtbw4j7mMTn.
6. Commit the screenshots and README with the change; give Ed the link in one line.
