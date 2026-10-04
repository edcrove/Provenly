# Provenly — agent guide

Provenly links test cases, CI results and history through a stable `TC-<id>` identity. Go modular monolith
(`catalog`, `execution`, `ingestion`) + React 19 frontend, contract-first REST (`api/openapi.yaml`), PostgreSQL.
Owner: **Ed** (product owner and approver). Read `README.md`, `CONTRIBUTING.md` and `docs/architecture.md` before
changing code; this file holds the working rules that are not obvious from the code.

## Talking to Ed

- Spanish (Uruguay) or US English, whichever he writes in. Extremely concise: answer first, no preamble, no recap,
  plain prose unless structure helps. No follow-up offers unless a decision is pending.
- Times in **UYT (UTC−3)**, never raw UTC.
- **Verify before reporting state.** Before saying something is done or pending (cards, PRs, branches, decisions,
  Notion pages), check the live source (Trello, GitHub, Notion, git). Never answer from memory.
- Product, domain, UX and scope decisions are Ed's: present findings with a recommendation and wait. Do not re-ask
  what is already decided (see the Decision Register and `docs/implementation-decisions.md`).

## Sources of truth

| What | Where |
|---|---|
| Why (decisions, scope, future ideas) | Notion: *Provenly — Consolidated Workspace*, **Decision Register**, **Incubator** |
| What (contract, schema, CI, docs) | This repo: `api/openapi.yaml`, `backend/migrations`, `.github/workflows`, `docs/` |
| Execution (cards and their state) | Trello board **Provenly**: Por hacer → En progreso → Validation → Hecho |

- Every decision Ed takes goes to the **Notion Decision Register** and, when it changes behavior, to
  `docs/implementation-decisions.md` (and `docs/mvp-plan.md` for plan decisions). Use the `record-decision` skill.
- Ideas not refined enough for the backlog go to Notion **Incubator**, not Trello.
- Notion migration rule: **never delete or alter an original before the copy is validated**. The Notion plan is
  rate-limited: batch reads and writes.

## Card workflow (Trello)

1. Claude moves a card to **En progreso** when starting and to **Validation** when the Definition of Done is met,
   with an evidence comment (what was done, how to verify, tests/CI evidence, code paths).
2. Validation: Claude reviews the card by hand (skill `validate-card`) and reports findings.
   **Standing authorization (Ed, 2026-10-03): fix what you find** during validation, re-test, commit and push,
   then report. Product/scope questions still go to Ed.
3. **Only Ed approves Hecho.** After his OK, Claude moves the card with an evidence comment.
4. **Validation produces regression tests (Ed, 2026-10-03).** Every check exercised by hand during a validation —
   passing or failing — ends up as an automated test in the layer it belongs to: unit for parsing, validation and pure logic; integration for database invariants, queries, concurrency and
  performance (relative bounds, never wall-clock thresholds alone); contract for HTTP statuses, shapes and limits
  (`offContract` for routes outside the spec); frontend integration/E2E for UI behavior; `scripts/probe/edge_cases.py`
  for API sweeps (it runs in CI). Prove a new test
   fails without its fix. A card is not ready for Ed's OK while a manual check has no automated counterpart; the
   evidence comment maps each manual check to its test id.

## Definition of Done (every card)

OpenAPI first (`npm run gen:api` after spec changes); new goose migration for schema changes (never edit a committed
migration); tests in **every layer touched, both sides** (unit, integration, contract, E2E; new test ids in
`coverage/inventories/*.yaml`; every manual validation check automated, see Card workflow 4); `make lint` clean; `make coverage` **PASS** (8 gates + 2 consolidated, all at 100%);
docs updated (`docs/architecture.md`, `docs/review.md`, decisions); `make screenshots` regenerated for UI changes
(`docs/screenshots/README.md` lists them); manual validation done.

## Commands

```bash
make lint            # gofmt, go vet, golangci-lint, eslint, prettier, tsc
make generate        # sqlc + OpenAPI TS client (never hand-edit generated code)
make check-generated # drift check (CI)
make fuzz            # Go fuzzers (FUZZTIME=10s each)
make coverage        # every layer + the 10 gates (needs Docker); prints "Overall: PASS"
make screenshots     # docs/screenshots on an ephemeral DB
make dev-backend / make dev-frontend   # non-docker dev (reads .env.local, not .env)
scripts/doctor.sh [--fix]              # environment check (pinned tools, packages, docker, chromium, ports)
make probe [BASE=URL]                  # edge-case sweep (scripts/probe/edge_cases.py); also runs in CI (docker job)
scripts/readme-flow.sh [API]           # README curl walkthrough, verbatim, on a fresh demo (CI docker job)
scripts/env-checks.sh                  # environments: isolation, seeds, dump/restore, prod guards (CI only)
scripts/gallery/build.py --out DIR     # click-through gallery of docs/screenshots, publishable as an Artifact
```

Project skills (`.claude/skills/`): `implement-card`, `validate-card`, `record-decision`, `steward`,
`project-status` (live status, never from memory), `ui-gallery`, `edge-case-probe`, `notion-safe-edit`,
`parity-analysis`, `close-milestone`, `env-doctor`.

Environment gotchas (web sessions; `.claude/hooks/session-start.sh` prepares most of this):

- Tool versions are pinned in `.tool-versions`. Use the sqlc and golangci-lint in `$(go env GOPATH)/bin` (built
  with the pinned Go); the ones in `/usr/local/bin` are older and break lint/generate.
- Docker dies between shells: `docker info || (setsid nohup dockerd >/tmp/dockerd.log 2>&1 &)`.
- Playwright: never `playwright install`; `PLAYWRIGHT_CHROMIUM_EXECUTABLE` points at `/opt/pw-browsers`.
- Stop servers with `fuser -k <port>/tcp`; `pkill -f` can kill your own shell.
- Prettier only under `frontend/` (its config). `e2e/` has no Prettier config: format by hand.

## Engineering rules

- API: problem+json errors with stable `code`; unknown query params ignored, known ones validated (present-but-empty
  is 400, first repeated value wins); JSON bodies need `Content-Type: application/json` (415); text must be valid
  UTF-8 without NUL (400); invalid ids/pages never reach the database as 500s. See `docs/architecture.md`.
- Runs: `executionStatus` (completed | interrupted | cancelled, reported by CI) is separate from the derived
  `outcome.verdict` (no_tests > failed > incomplete > passed) and `passRate` (% of executed that passed).
- Security: the repo is **public**. Never commit secrets; `envs/prod.env` and `backups/` are gitignored and off-limits.
- Git: work on the session's assigned branch; never force-push, never push to `main` (protected, PR only, Ed
  merges); no model identifiers in commits, PRs or code.

## Guardrails enforced by hooks (`.claude/settings.json`)

`guard.sh` denies: edits to generated code (sqlc `*db/*.go`, `frontend/src/api/schema.d.ts`), edits to committed
migrations, touching `envs/prod.env` / `backups/`, force pushes, pushes to `main`, `--no-verify`, interactive
rebase, `pkill -f`, and Prettier on `e2e/`. `format.sh` runs gofmt / the frontend Prettier after each edit.
