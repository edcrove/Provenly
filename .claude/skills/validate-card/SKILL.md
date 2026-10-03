---
name: validate-card
description: Manually validate a Provenly Trello card that is in Validation — review every acceptance criterion against the real running system (API, Postgres, Chromium), hunt edge cases, fix what is found, and get Ed's approval to move it to Hecho. Use when Ed says to validate, review or continue with a card ("validemos", "seguí con la próxima card", "revisá la card X").
---

# Validate a card

Ed validates cards one by one. Claude does the hands-on review; only Ed approves Hecho.

## 1. Read the card and its history
- Trello board **Provenly**, list **Validation**: read the card (description = acceptance criteria) and every
  comment (decisions taken after the fact change the criteria).
- Find the code, tests and decisions it points to (`docs/review.md` traceability matrix, `docs/implementation-decisions.md`).

## 2. Exercise the real system
- Fresh Postgres (`docker run -d --name pv-manual -e POSTGRES_USER=provenly -e POSTGRES_PASSWORD=provenly
  -e POSTGRES_DB=provenly -p 5450:5432 postgres:16-alpine`), the API built from the branch
  (`PROVENLY_AUTO_MIGRATE=true`), the UI with `npm run dev`. Drive the UI with Playwright + Chromium
  (`$PLAYWRIGHT_CHROMIUM_EXECUTABLE`), the API with curl/python, and check the database directly with psql.
- Prove each acceptance criterion with an observable result, not by reading code.
- Then go past the criteria. Checklist of things that have broken before:
  - boundaries: empty, whitespace, max length ±1, huge numbers (page/offset overflow), ids `0`, `-1`, `abc`, `1.5`, max int64
  - text: emoji/multibyte, HTML/`<script>` (must render escaped), NUL `\u0000`, invalid UTF-8
  - HTTP: wrong/missing Content-Type, unknown/duplicate/empty params and JSON keys, cross-resource ids (a child of another parent)
  - concurrency: double click / double submit, N parallel requests (limits and ordering must hold)
  - UI: page past the end, filters with no matches, not-found and error states, tab titles, keyboard, 375 px width (no page-level horizontal scroll)
- Run the `edge-case-probe` skill against the same stack. Any 500 is a finding.
- Keep a list of every check you run by hand (input → expected → observed). Each one becomes a regression test in
  step 3, whether it passed or failed.

## 3. Report, then fix
- Report to Ed concisely: which criteria pass (with the evidence), and the findings with a proposed fix each.
- Standing authorization (Ed, 2026-10-03): **fix what you find**, then report. Ask first only for product/scope
  choices (behavior Ed has not decided).
- Every fix: a test that fails without it (prove it by disabling the fix once), every layer touched, `make lint`,
  `make coverage` PASS, re-run the manual probe against the real system, add a row to the findings table in
  `docs/review.md`, regenerate screenshots if the UI changed. Commit, push, wait for CI green.
- **Automate every manual check (rule, Ed 2026-10-03)**, not only the ones that failed: unit for parsing, validation and pure logic; integration for database invariants, queries, concurrency and
  performance (relative bounds, never wall-clock thresholds alone); contract for HTTP statuses, shapes and limits
  (`offContract` for routes outside the spec); frontend integration/E2E for UI behavior; `scripts/probe/edge_cases.py`
  for API sweeps (it runs in CI).
  Before asking for Hecho, walk the list from step 2 and confirm each check has a test id; add what is missing.

## 4. Close
- Ask Ed: "¿Paso <card> a Hecho?". Only after his explicit OK: add an evidence comment (criteria verified, fixes
  with commit hashes, the manual check → test id mapping, gates, CI) and move the card to **Hecho**.
- If Ed asks to see the flows, use the `ui-gallery` skill.
