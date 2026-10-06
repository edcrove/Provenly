# provenly-playwright-reporter

The first native Provenly reporter (DEC-15). While Playwright runs it starts a **live run** in Provenly and streams
`test.started` / `test.finished` events; when the run ends it sends the authoritative **JUnit report** (one testcase
per attempt, with the test's TC-ID and attempt) through the normal ingestion, which completes the live run and
reconciles it with the events. Provenly being unreachable never fails your tests: the reporter logs and carries on.

## Install

```bash
npm i -D provenly-playwright-reporter
```

Published unscoped (the `@provenly` npm scope is not this project's) with npm provenance, from this repository's
release workflow; the release notes give the npm version of each tag. Before a release, install it from a checkout:
`(cd /path/to/Provenly/reporters/playwright && npm ci && npm run build) && npm i -D /path/to/Provenly/reporters/playwright`.

## Use

```ts
// playwright.config.ts
export default defineConfig({
  reporter: [['list'], ['provenly-playwright-reporter', { project: 'CHK' }]],
})
```

Environment (CI): `PROVENLY_URL`, `PROVENLY_API_KEY` (a project API key), optional `PROVENLY_PROJECT` and
`PROVENLY_SUITE`. On GitHub Actions the run id, attempt, workflow, branch and commit come from `GITHUB_*`. On any other
CI pass them as options from its own variables (for example GitLab:
`{ provider: 'gitlab', runId: process.env.CI_PIPELINE_ID, runAttempt: 1, branch: process.env.CI_COMMIT_REF_NAME,
commit: process.env.CI_COMMIT_SHA }`): otherwise the run id is a timestamp, so a re-sent report is a new run instead of
a replay. Without `PROVENLY_URL` the reporter does nothing.

Sharded runs (`npx playwright test --shard=2/4`, e.g. a CI matrix): each shard sends its own report with
`?shard=2/4` (from Playwright's `--shard`, or `PROVENLY_SHARD`/the `shard` option) and Provenly merges the reports into
one run, completed when every shard reported; live streaming is off for sharded runs. If a shard job may die before
reporting, end the run from a job that runs after the matrix (`if: always()`):

```yaml
- run: >-
    curl -fsS -X POST -H "Authorization: Bearer $PROVENLY_API_KEY"
    "$PROVENLY_URL/api/v1/ingestion/finalize?provider=github&runId=$GITHUB_RUN_ID&runAttempt=$GITHUB_RUN_ATTEMPT"
```

It ends a run still waiting for shards as `interrupted`, naming the missing ones (a run already complete is unchanged).

Alternatively merge the shards first (`npx playwright merge-reports` with the `junit` reporter) and send the merged
report once, without `shard`: Provenly then sees one ordinary run.

## Declaring the TC-ID of a test

The test case must exist in Provenly first (create it in the UI or through the API): a TC-ID that does not exist is
stored as `unknown` and the result is not linked to any test case. Any of, in this order:

```ts
test('pays by card', { annotation: { type: 'tc-id', description: 'CHK-12' } }, async () => {})
test('pays by card', { tag: '@CHK-12' }, async () => {})
test('pays by card CHK-12', async () => {})
```

## Options

`url`, `apiKey`, `project`, `suite`, `provider`, `runId`, `runAttempt`, `pipeline`, `branch`, `commit`, `shard` override
the environment; `live: false` only sends the final report; `flushEvery` (default 50) sets the event batch size.

## Develop

`npm ci && npm run typecheck && npm test` — the unit tests must keep 100% statements, branches, functions and lines.
