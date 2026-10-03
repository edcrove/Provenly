# Testing & coverage strategy (POC)

Backend and frontend each have Unit, Integration, Contract and E2E layers: **8 independent gates**. Each gate must
reach 100% of *its own* denominator; `covgate` evaluates them one by one and a consolidated number never replaces a
gate.

## What "100%" means per gate

| Gate | Denominator (authoritative source) | Evidence read by `covgate` |
|---|---|---|
| backend-unit | Go statements of every product package (`go test -cover -coverpkg=./...`). Packages no test links are reported as errors, so code cannot silently leave the denominator. | `coverage/out/backend-unit.out` (from `GOCOVERDIR` via `go tool covdata textfmt`) |
| frontend-unit | statements **and** branches of every `src/**` file (`@vitest/coverage-v8`, `include: src/**`) | `frontend/coverage/unit/coverage-final.json` |
| backend-integration | every sqlc query (auto-derived from `backend/queries/*.sql`, covered when its generated method ran against Postgres) + `coverage/inventories/backend-integration.yaml` | integration coverage profile + `go test -json` |
| frontend-integration | `coverage/inventories/frontend-integration.yaml` + every UI surface (`.tsx` under `src/app`, `src/features`, `src/components`) must be referenced by a target | Vitest JSON report |
| backend-contract | every `operationId × declared status` in `api/openapi.yaml` | `coverage/out/backend-contract.json` (pairs validated by kin-openapi) |
| frontend-contract | every declared status of every operation the frontend consumes (auto-derived from the generated-client calls in `src/api/queries.ts`) | `frontend/coverage/contract/evidence.json` (pairs validated with ajv against the spec) |
| backend-e2e / frontend-e2e | `coverage/inventories/{backend,frontend}-e2e.yaml` journeys | Playwright JSON report (`[BE-E2E-xxx]` / `[FE-E2E-xxx]` in test titles) |

A target is covered only when at least one test carrying its id passed **and none failed**.

### Consolidated gates (all layers merged)

Exceptions only absorb elements that are not covered. On top of the 8 layer gates, two **consolidated gates**
require that *all* code is executed by some layer, on the coverage of every layer merged — so anything a Unit gate
excepts must be covered by another layer:

| Gate | Denominator | Merged evidence |
|---|---|---|
| backend-consolidated | every statement of the backend module | unit + integration + contract + e2e raw coverage (`go tool covdata textfmt` over every GOCOVERDIR) |
| frontend-consolidated | every statement line of `frontend/src` | unit + integration (v8) + e2e (istanbul, remapped through source maps to original lines) |

Lines no layer can execute are `*-consolidated` exceptions (`not-reachable`, `test-support`); every line they absorb
is listed. The per-file report (`coverage/out/consolidated-coverage.md`, backend HTML
`coverage/out/backend-consolidated.html`) shows covered / reachable / required / effective %, uncovered lines (must be
empty) and excepted lines.

### Gate denominator vs code-coverage evidence

Code coverage collected during Integration, Contract and E2E (`GOCOVERDIR` of the tests and of the
`go build -cover` binary; `vite-plugin-istanbul` bundle during Playwright) is **published as evidence** in the
report ("Code coverage evidence" table, including consolidated backend statements and frontend lines). It is never
the gate for those layers: E2E is not required to execute 100% of statements.

### Backend branches

Go's native tooling measures statements only. The backend Unit gate therefore gates on statements and reports a
complementary *branch inventory* (number of decision points in the unit scope) as information; it does not claim
branch coverage.

## Report format

Per gate and metric: `reachableTotal`, `covered`, `exceptions`, `requiredTotal = reachableTotal − exceptions`,
`gaps`, `effectiveCoverage = covered / requiredTotal`, `rawCoverage = covered(all) / reachableTotal`, target 100%.
Gaps are listed with their location/target id. Output: terminal, `coverage/out/coverage-report.{md,json}` and the
GitHub Actions step summary.

Because every gate requires 100% of its required denominator, coverage cannot regress without failing CI; the
denominator cannot shrink silently either (auto-derived targets, surface discovery, missing-package check, stale
exception check).

## Discovering targets

- New SQL query → new backend-integration target automatically.
- New OpenAPI operation or response status → new backend-contract target; if the frontend calls it, a new
  frontend-contract target.
- New `.tsx` UI surface → gap until an `FE-INT` target references it.
- New integration behavior / journey → add it to the matching inventory (reviewed like code), then tag the test.

## Exceptions

`coverage/exceptions.yaml` is the only way to remove elements from a required denominator. Fields: `id, gate, layer,
side, target (glob or target id, also file:line), reason, category, evidence, owner, createdAt, reviewBy, link,
status`. Categories: `generated-code`, `other-layer`, `vendored-ui`, `entrypoint`, `not-reachable`,
`tooling-limitation`, `test-support`. To approve one: open a PR adding the entry with `status: approved` and a
`reviewBy` date; the reviewer checks that `evidence` really verifies it elsewhere. CI fails on missing fields,
unknown category, non-approved status, expired `reviewBy` and stale targets. Exceptions never disappear from reports.

## Commands

```bash
make test-backend-unit | test-backend-integration | test-backend-contract
make test-frontend-unit | test-frontend-integration | test-frontend-contract
make test-e2e            # starts its own ephemeral database
make gates               # evaluate the 8 gates from collected evidence
make coverage            # everything above
```

## Spike: unifying Go raw coverage (Go 1.26, re-verified on 1.27.1)

Verified in this repository (not assumed):

- `go test -cover -covermode=atomic -coverpkg=./... <pkgs> -args -test.gocoverdir=DIR` writes raw counters to `DIR`
  with Go 1.26; `GOCOVERDIR` alone is ignored by `go test`, so `-test.gocoverdir` is required.
- Running `go test -cover` on packages **without test files** prints `go: no such tool "covdata"` in Go 1.26
  toolchains (the `covdata` binary is not pre-built). The Makefile therefore passes only packages that have tests and
  relies on `-coverpkg=./...` to instrument the rest.
- Go 1.27 starts some coverage blocks at their first statement instead of the opening brace (e.g. a function body
  or an `if` body), so line-targeted exceptions must point at the statement line; the stale-exception check catches
  any drift after a toolchain bump.
- The E2E binary is built with `go build -cover -covermode=atomic -coverpkg=./...` and run with `GOCOVERDIR`; it
  flushes counters on `os.Exit` after a graceful SIGTERM. The covermode **must match** (`atomic`) across unit,
  integration, contract and E2E, otherwise `go tool covdata` fails with "counter mode clash".
- `go tool covdata textfmt -i=unit,integration,contract,e2e -o merged.out` merges all of them (used for the
  consolidated evidence and the backend-consolidated gate).
- Frontend E2E coverage from `vite-plugin-istanbul` is measured on transformed code (JSX collapsed into single
  lines) with an embedded source map; a Playwright global teardown remaps it with `istanbul-lib-source-maps` before
  it is merged with the Vitest v8 coverage (`e2e/coverage/frontend-remapped`).

E2E and screenshots use Playwright's own Chromium build (`npx playwright install chromium`). To reuse a Chromium
already installed on the machine, set `PLAYWRIGHT_CHROMIUM_EXECUTABLE=/path/to/chrome`.
