# @provenly/playwright-reporter

The first native Provenly reporter (DEC-15). While Playwright runs it starts a **live run** in Provenly and streams
`test.started` / `test.finished` events; when the run ends it sends the authoritative **JUnit report** (one testcase
per attempt, with the test's TC-ID and attempt) through the normal ingestion, which completes the live run and
reconciles it with the events. Provenly being unreachable never fails your tests: the reporter logs and carries on.

## Use

```ts
// playwright.config.ts
export default defineConfig({
  reporter: [['list'], ['@provenly/playwright-reporter', { project: 'CHK' }]],
})
```

Environment (CI): `PROVENLY_URL`, `PROVENLY_API_KEY` (a project API key), optional `PROVENLY_PROJECT` and
`PROVENLY_SUITE`. On GitHub Actions the run id, attempt, workflow, branch and commit come from `GITHUB_*`. Without
`PROVENLY_URL` the reporter does nothing.

## Declaring the TC-ID of a test

Any of, in this order:

```ts
test('pays by card', { annotation: { type: 'tc-id', description: 'CHK-12' } }, async () => {})
test('pays by card', { tag: '@CHK-12' }, async () => {})
test('pays by card CHK-12', async () => {})
```

## Options

`url`, `apiKey`, `project`, `suite`, `provider`, `runId`, `runAttempt`, `pipeline`, `branch`, `commit` override the
environment; `live: false` only sends the final report; `flushEvery` (default 50) sets the event batch size.

## Develop

`npm ci && npm run typecheck && npm test` — the unit tests must keep 100% statements, branches, functions and lines.
