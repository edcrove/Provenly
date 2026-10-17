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
`PROVENLY_SUITE`. On GitHub Actions the run id, attempt, workflow, branch, commit and the link to the run page (`ciUrl`)
come from `GITHUB_*`. On any other
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

A CI **matrix that is not a shard** (one job per browser or OS, each running the whole suite) is a replay of the same
run id: the first job creates the run, the others are answered `200` with a warning ("not applied") and their results
are merged by test name. Give each job its own `suite` or `runId` (for example the GitHub run id plus the browser name)
when you want one run per job. The reporter logs the server's warnings, so a silent replay is visible in the job log.

When the report is accepted the reporter logs one line: the run's number and page, how many results were received and
persisted, and how many have no valid TC-ID (the run page lists them under *Diagnostics*).

## Declaring the TC-ID of a test

The test case must exist in Provenly first (create it in the UI or through the API): a TC-ID that does not exist is
stored as `unknown` and the result is not linked to any test case. Any of, in this order:

```ts
test('pays by card', { annotation: { type: 'tc-id', description: 'CHK-12' } }, async () => {})
test('pays by card', { tag: '@CHK-12' }, async () => {})
test('pays by card CHK-12', async () => {})
```

In a title only the project's own key counts when the reporter knows the project (`project` option or
`PROVENLY_PROJECT`): `UTF-8`, `ISO-8601` or `HTTP-2` are words. Without a project any `KEY-n` is taken, so prefer the
annotation or the tag there.

## Options

`url`, `apiKey`, `project`, `suite`, `provider`, `runId`, `runAttempt`, `pipeline`, `branch`, `commit`, `ciUrl` (the CI
job's URL, shown on the run), `shard` override
the environment; `live: false` only sends the final report; `flushEvery` (default 50) sets the event batch size.

## Compatibility

The reporter sends `ciUrl` when it starts a live run; a Provenly older than v0.3.0 rejects that start (400) and only the
live view is lost: the final report is still sent and recorded. Use the reporter released with your Provenly.

## Develop

`npm ci && npm run typecheck && npm test` — the unit tests must keep 100% statements, branches, functions and lines.
