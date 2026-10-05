# Provenly — full-product exploration prototype

Branch `prototype/full-product` (from `main` at 5245a4c, 2026-10-05). Ed asked for the **whole product** built from
everything written so far (Trello backlog, Notion Incubator, Planning Backlog, Decision Register, product pages),
including ideas that were never refined, **without his intervention**: every open question is decided here, with the
reasoning, so the prototype can be reviewed afterwards and the real backlog refined from it.

Rules kept from `main` (CLAUDE.md): contract first, a goose migration per schema change, tests in every layer touched on
both sides, the 8 + 2 coverage gates at 100%, `make lint` clean. One PR per feature into this branch, self-reviewed.
Nothing here is merged into `main`.

Status legend: ✅ merged into the prototype branch · 🚧 in progress · ⏳ planned.

## Feature map

| # | Feature | Source | Status | PR |
|---|---|---|---|---|
| 1 | Projects and per-project TC keys (`CHK-12`) | MVP D9, D10 | ✅ | proto/01-projects |
| 2 | Users, login (JWT) and invitations | MVP D13, DEC-30 | ⏳ | |
| 3 | Roles and project membership (Admin, Maintainer, Member, Viewer) | MVP D12 | ⏳ | |
| 4 | API keys for CI, `?project=` ingestion, secrets at rest | MVP D4, D11 | ⏳ | |
| 5 | Optimistic locking (ETag / If-Match) | MVP D7 | ⏳ | |
| 6 | Snapshot amendment per run | DEC-42 | ⏳ | |
| 7 | Retries, logical result and flaky | MVP D1 | ⏳ | |
| 8 | Compressed (gzip) report ingestion | MVP D6 | ⏳ | |
| 9 | Taxonomy: tags and custom dimensions | Planning #26 | ⏳ | |
| 10 | Test suites (static and query) and partial-run scope | Planning #27, MVP D2, Incubator | ⏳ | |
| 11 | Manual execution (manual runs, step results) | MVP D3, Planning #4, Incubator | ⏳ | |
| 12 | Requirements and requirement ↔ test traceability | Incubator (Requirements Federation) | ⏳ | |
| 13 | Issues, known issues and issue verification | Incubator, Planning #8 | ⏳ | |
| 14 | Quality dashboard (trends, flaky, coverage) | Incubator (Quality Intelligence) | ⏳ | |
| 15 | Live runs: execution sessions, live events, reconciliation | Trello Live Streaming, Planning #9 | ⏳ | |
| 16 | Playwright reporter (`@provenly/playwright-reporter`) | Trello, DEC-15 | ⏳ | |
| 17 | OpenTelemetry basic instrumentation | Trello, DEC-11 | ⏳ | |
| 18 | Export sink (webhooks) and GitHub connector | Planning #3, #21, Incubator | ⏳ | |
| 19 | MCP server (agent interface) | Incubator, DEC-10 | ⏳ | |
| 20 | Audit log | Incubator (Project & Authorization) | ⏳ | |
| 21 | Release pipeline, self-hosting guide, dogfooding, public readiness | Trello phase 4 | ⏳ | |

## Decision log

Decisions taken in the prototype without Ed (to review). `MVP Dn` and `DEC-n` are Ed's decisions, applied as written.

| Id | Topic | Decision | Why |
|---|---|---|---|
| P1-1 | Project key format | 2–10 upper-case letters/digits starting with a letter; trimmed and upper-cased on create; never changes; projects are never deleted | Keys are printed in test names and reports, so they must be stable and readable like Jira keys |
| P1-2 | Existing data | Default project `TC` (id 1) holds every test case created before projects; existing ids become numbers (`TC-153` stays `TC-153`); its counter continues after the highest id | No reference already written in a test or report breaks |
| P1-3 | URLs and internal ids | REST paths keep the numeric internal id (`/test-cases/{id}`); `key`, `projectKey` and `number` are added to the payload | Smallest change to the contract; the key is for humans and reports, the id for links |
| P1-4 | Numbering | Per-project counter incremented in the same statement as the insert (row lock); contiguous under concurrency, never reused | Same guarantees as the old identity column, now per project |
| P1-5 | Ingestion | `?project=<KEY>` (default `TC`) chooses the run's project and its expected universe; unknown key 404 | MVP D11 (ingestion project as a parameter) |
| P1-6 | References | A `tc-id` property may carry any key; another project's key is a new `wrong_project` diagnostic, never correlated. The name fallback only reads the run's own key; a bare number belongs to the run's project | Avoids silent cross-project links and false positives in test names (`HTTP-200`) |
| P1-7 | Run identity | `externalRunId` unique per project | One CI pipeline may report several projects |
| P1-8 | Keys in run responses | Results, summaries and history carry `testCaseKey` (resolved by the catalog through its public interface) | Runs reference internal ids; showing `TC-<id>` would be wrong for other projects |
| P1-9 | UI | Header "current project" selector (remembered per browser) narrows test case and run lists; Projects page creates and renames projects; new test cases pick a project | Single place to switch context; no URL change needed for the prototype |

## What each feature does

Filled in as each feature is merged: behavior, API, UI, tests, known limits.

### 1. Projects and per-project TC keys

- **Behavior**: projects own test cases and runs; each numbers its test cases with its key (`CHK-1`, `CHK-2`…).
  Runs ingested with `?project=CHK` snapshot CHK's universe and only link CHK test cases; a `tc-id` with another
  project's key is reported as `wrong_project`.
- **API**: `GET/POST /api/v1/projects`, `GET/PATCH /api/v1/projects/{projectKey}` (409 on a key in use);
  `?project=` on `GET /test-cases`, `GET /test-runs` and the ingestion; `project` in `POST /test-cases`;
  `projectId`/`projectKey`/`number` on test cases, `projectId` on runs, `testCaseKey` on results and summaries,
  `wrongProject` in diagnostic counts.
- **Schema**: migrations 00012 (projects, numbering, protection triggers, backfill) and 00013 (run project,
  per-project run ids, `wrong_project`).
- **UI**: Projects page, current-project selector in the header, project picker on new test case, project column in
  the run list, keys from the API everywhere (screenshots 36–38).
- **Tests**: BE-INT-031..035, contract scenarios for every new operation × status (backend and frontend), FE-INT-022..025,
  BE-E2E-007, FE-E2E-010, unit tests (catalog, execution, ingestion, junit, httpx, apperr, frontend storage), probe
  sweep for projects.
- **Known limits**: no project-level permissions yet (feature 3); a test case cannot move between projects; the
  project list in the header loads up to 100 projects.
