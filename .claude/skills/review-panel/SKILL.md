---
name: review-panel
description: Run Provenly's review panel — technical expert subagents (API contract, database, security, tests, frontend, domain) on a diff or PR, the architect and product owner on the whole project, and/or user persona subagents (QA lead, developer, manual tester, DevOps, engineering manager) on the UI flows — then consolidate, verify and report their findings. Use before opening or merging a PR, when validating a card, after UI changes, or when Ed asks for a review "con los agentes", "con los expertos" or "con las personas".
---

# Review panel

The agents live in `.claude/agents/` and are read-only: they report, Claude verifies and fixes.

## 1. Pick the panel
- Target: the PR or `git diff origin/<base>...HEAD` (base `prototype/full-product` while the prototype is open,
  else `main`). List the changed paths.
- Technical experts, by what the diff touches (run every one that applies, in parallel, in the background):

  | Touches | Agent |
  |---|---|
  | `api/openapi.yaml`, `*/http.go`, `frontend/src/api` | `reviewer-api-contract` |
  | `backend/migrations`, `backend/queries`, `*/postgres/*.go` | `reviewer-database` |
  | identity, integrations, mcp, audit, ingestion, config, workflows, dependencies | `reviewer-security` |
  | any code change | `reviewer-tests` |
  | `frontend/src` | `reviewer-frontend` |
  | catalog, execution, ingestion, insights behavior | `reviewer-domain` |

- Feature coverage audit: when validating a card, closing a milestone or when Ed asks whether a feature is fully
  tested, run `reviewer-tests` in Mode 2 per feature (one agent per feature, in parallel): it plans the tests of
  the acceptance criteria from zero, then maps them to the existing tests and lists every gap. Missing tests it
  reports are written (Card workflow 4) and proven to fail without the code they guard.
- Whole-product reviews (before closing a milestone, after a large batch of features, or when Ed asks):
  `architect` for the codebase (or a module) and `product-owner` for vision fit, drift and future ideas. They do
  not need a diff; give them the scope.
- Personas, for UI or flow changes and before closing a milestone: regenerate `make screenshots` first, then run
  the personas whose journeys the change touches (all five for a milestone).

## 2. Brief each agent
Give each one the target (PR number or base), the card or goal in one sentence, and any decision it must respect
(link the Pxx/DEC entry). Ask for its standard output format. Do not paste the diff: the agent reads it.

## 3. Consolidate and verify
- Merge duplicates; keep the strongest severity.
- Verify every blocker and major yourself (reproduce, read the code, run the test). Mark each finding
  `confirmed`, `not reproducible` or `needs Ed` (product, scope or UX decisions are Ed's).
- Product-owner suggestions are proposals for Ed and the Notion Incubator, never implemented directly; architect
  refactors larger than a local fix become cards (or a proposal to Ed), not drive-by changes.
- Confirmed findings are fixed under the standing authorization (Card workflow 2 in `CLAUDE.md`) with a
  regression test that fails without the fix (Card workflow 4); persona frictions that change product behavior or
  wording beyond a clear defect go to Ed with a recommendation.

## 4. Report
To Ed, concise: panel run (agents), confirmed findings fixed (with test ids), findings needing his decision (with
a recommendation), dismissed ones in one line each. Persona results also go into the card's evidence comment.
