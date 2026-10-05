import { apiURL } from '../playwright.config'
import { byProperty, expect, junit, test, uniqueProjectKey, uniqueRunId } from '../support/fixtures'

test.describe('Live runs', () => {
  test('[BE-E2E-021] a live run through the public API: started, events streamed idempotently, completed by its report and reconciled', async ({ request, provenly }) => {
    const key = uniqueProjectKey()
    await provenly.createProject(key, 'Live API')
    const login = await provenly.createTestCase({ title: 'login', project: key, automated: true })
    const pay = await provenly.createTestCase({ title: 'pay', project: key, automated: true })
    const runId = uniqueRunId()
    const start = { project: key, provider: 'github', runId, runAttempt: 1, pipeline: 'ci' }
    const started = await request.post(`${apiURL}/api/v1/test-runs/live`, { data: start })
    expect(started.status()).toBe(201)
    const run = await started.json()
    expect(run).toMatchObject({ mode: 'live', executionStatus: 'running', expectedCount: 2 })
    expect((await (await request.post(`${apiURL}/api/v1/test-runs/live`, { data: start })).json()).id).toBe(run.id)

    const events = (list: object[]) => request.post(`${apiURL}/api/v1/test-runs/${run.id}/events`, { data: { events: list } })
    const first = [
      { eventId: 'e1', sequence: 1, type: 'test.started', testName: 'login', testCase: login.key },
      { eventId: 'e2', sequence: 2, type: 'test.finished', testName: 'login', testCase: login.key, status: 'passed' },
      { eventId: 'e3', sequence: 3, type: 'test.started', testName: 'pay', testCase: pay.key },
    ]
    expect(await (await events(first)).json()).toEqual({ accepted: 3, duplicates: 0 })
    expect(await (await events(first)).json()).toEqual({ accepted: 0, duplicates: 3 })
    expect((await events([{ eventId: 'x', sequence: 1, type: 'test.finished' }])).status()).toBe(400)
    const progress = await (await request.get(`${apiURL}/api/v1/test-runs/${run.id}/live`)).json()
    expect(progress).toMatchObject({ reconciliation: 'pending', finished: 1, running: 1, waiting: 0 })

    const report = await request.post(`${apiURL}/api/v1/ingestion/junit?project=${key}&provider=github&runId=${runId}&runAttempt=1`, {
      headers: { 'Content-Type': 'application/xml' },
      data: junit(byProperty('login', login.key), byProperty('pay', pay.key, '<failure message="x"/>')),
    })
    expect(report.status()).toBe(201)
    expect((await report.json()).testRun).toMatchObject({ id: run.id, executionStatus: 'completed', outcome: { verdict: 'failed' } })
    const reconciled = await (await request.get(`${apiURL}/api/v1/test-runs/${run.id}/live`)).json()
    expect(reconciled.reconciliation).toBe('mismatch')
    expect(reconciled.mismatches).toEqual([
      { kind: 'started_without_finished', testCaseId: pay.id, testCaseKey: pay.key, requestedTestCaseId: null, liveStatus: null, finalStatus: 'failed' },
    ])
    expect((await events([{ eventId: 'late', sequence: 9, type: 'run.finished' }])).status()).toBe(409)
  })

  test('[FE-E2E-023] the run page follows a live run while CI streams events and shows the reconciliation once the report arrives', async ({ page, request, provenly }) => {
    const key = uniqueProjectKey()
    await provenly.createProject(key, 'Live UI')
    const login = await provenly.createTestCase({ title: 'Sign in works', project: key, automated: true })
    const pay = await provenly.createTestCase({ title: 'Pay works', project: key, automated: true })
    const runId = uniqueRunId()
    const run = await (await request.post(`${apiURL}/api/v1/test-runs/live`, { data: { project: key, provider: 'github', runId, runAttempt: 1 } })).json()
    await request.post(`${apiURL}/api/v1/test-runs/${run.id}/events`, {
      data: { events: [{ eventId: 'a', sequence: 1, type: 'test.started', testName: 'sign in', testCase: login.key }] },
    })

    await page.goto(`/test-runs/${run.id}`)
    await expect(page.getByTestId('live-progress')).toContainText('0 of 2 finished · 1 running · 1 waiting')
    await expect(page.getByTestId(`live-${login.key}`)).toContainText('running')
    await expect(page.getByTestId('verdict-badge').first()).toContainText('so far')
    await request.post(`${apiURL}/api/v1/test-runs/${run.id}/events`, {
      data: { events: [{ eventId: 'b', sequence: 2, type: 'test.finished', testName: 'sign in', testCase: login.key, status: 'passed' }] },
    })
    await expect(page.getByTestId(`live-${login.key}`)).toContainText('passed')
    await expect(page.getByTestId('reconciliation')).toHaveText('awaiting final report')

    const report = await request.post(`${apiURL}/api/v1/ingestion/junit?project=${key}&provider=github&runId=${runId}&runAttempt=1`, {
      headers: { 'Content-Type': 'application/xml' },
      data: junit(byProperty('sign in', login.key), byProperty('pay', pay.key)),
    })
    expect(report.status()).toBe(201)
    await expect(page.getByTestId('reconciliation')).toHaveText('mismatch')
    await expect(page.getByTestId('mismatch')).toContainText('In the report, never seen live')
    await expect(page.getByTestId('verdict-badge').first()).toHaveText('passed')
    await expect(page.getByTestId('live-progress')).toContainText('As streamed live: 1 of 2 finished · 0 still running · 1 never started')
  })
})
