import { apiURL } from '../playwright.config'
import { byName, byProperty, expect, junit, test, uniqueRunId } from '../support/fixtures'

test.describe('Backend API journeys', () => {
  test('[BE-E2E-001] health check responds through the public interface', async ({ request }) => {
    const res = await request.get(`${apiURL}/healthz`)
    expect(res.status()).toBe(200)
    expect(await res.json()).toEqual({ status: 'ok' })
  })

  test('[BE-E2E-002] test case lifecycle: server-assigned immutable TC-ID, edit and deprecate', async ({ request, provenly }) => {
    const tc = await provenly.createTestCase({ title: 'API lifecycle', automated: true })
    expect(tc.key).toBe(`TC-${tc.id}`)
    const rejected = await request.post(`${apiURL}/api/v1/test-cases`, { data: { id: 1, title: 'forced id' } })
    expect(rejected.status()).toBe(400)
    expect((await rejected.json()).code).toBe('validation_error')

    const edited = await request.patch(`${apiURL}/api/v1/test-cases/${tc.id}`, { data: { title: 'API lifecycle v2' } })
    expect(await edited.json()).toMatchObject({ id: tc.id, key: tc.key, title: 'API lifecycle v2' })

    const deprecated = await request.post(`${apiURL}/api/v1/test-cases/${tc.id}/deprecate`)
    expect(await deprecated.json()).toMatchObject({ id: tc.id, status: 'deprecated' })
    const list = await request.get(`${apiURL}/api/v1/test-cases?status=deprecated&pageSize=100`)
    expect((await list.json()).items.map((t: { id: number }) => t.id)).toContain(tc.id)
    expect((await request.get(`${apiURL}/api/v1/test-cases/987654321`)).status()).toBe(404)
  })

  test('[BE-E2E-003] optional steps can be added, reordered, edited and deleted', async ({ request, provenly }) => {
    const tc = await provenly.createTestCase({ title: 'API steps' })
    const steps = `${apiURL}/api/v1/test-cases/${tc.id}/steps`
    expect((await (await request.get(steps)).json()).totalItems).toBe(0)
    const a = await (await request.post(steps, { data: { action: 'open' } })).json()
    const b = await (await request.post(steps, { data: { action: 'submit', expectedResult: 'ok' } })).json()
    const order = await request.put(`${steps}/order`, { data: { stepIds: [b.id, a.id] } })
    expect((await order.json()).items.map((s: { action: string }) => s.action)).toEqual(['submit', 'open'])
    await request.patch(`${steps}/${a.id}`, { data: { action: 'open app' } })
    expect((await request.delete(`${steps}/${b.id}`)).status()).toBe(204)
    const remaining = await (await request.get(steps)).json()
    expect(remaining.items).toMatchObject([{ id: a.id, position: 1, action: 'open app' }])
    expect((await request.get(`${apiURL}/api/v1/test-cases/${tc.id}`)).status()).toBe(200)
  })

  test('[BE-E2E-004] CI ingestion: TC-ID extraction, results, snapshot summary and diagnostics', async ({ request, provenly }) => {
    await provenly.isolateUniverse()
    const login = await provenly.createTestCase({ title: 'API login', automated: true })
    const logout = await provenly.createTestCase({ title: 'API logout', automated: true })
    const deprecated = await provenly.createTestCase({ title: 'API old', automated: true })
    await request.post(`${apiURL}/api/v1/test-cases/${deprecated.id}/deprecate`)

    const res = await provenly.ingest(
      uniqueRunId(),
      1,
      junit(
        byProperty('login chrome', login.id),
        byName(`login firefox TC-${login.id}`, '<failure message="timeout">stack</failure>'),
        byName('no tc id'),
        byName('broken TC-abc'),
        byName('ghost TC-987654321'),
        byName(`old TC-${deprecated.id}`),
        `<testcase name="login slow TC-${login.id}" time="soon"/>`,
        '<testcase name=""/>',
        `<testcase name="login instant TC-${login.id}" time="0"/>`,
      ),
    )
    expect(res.status()).toBe(201)
    const body = await res.json()
    expect(body).toMatchObject({ created: true, received: 9, persisted: 8 })
    expect(body.parseErrors.map((e: { persisted: boolean; severity: string }) => [e.persisted, e.severity])).toEqual([
      [true, 'error'],
      [false, 'error'],
      [true, 'warning'],
    ])
    expect(body.diagnostics.map((d: { correlation: string }) => d.correlation)).toEqual([
      'missing',
      'malformed',
      'unknown',
      'deprecated',
    ])

    const runId = body.testRun.id
    const summary = await (await request.get(`${apiURL}/api/v1/test-runs/${runId}/summary`)).json()
    expect(summary).toMatchObject({
      expectedTotal: 2,
      executedTotal: 1,
      counts: { untested: 1, passed: 0, failed: 1, error: 0, skipped: 0 },
      percentOfExpected: { untested: 50, failed: 50 },
      percentOfExecuted: { failed: 100 },
      executionPercent: 50,
      diagnostics: { missing: 1, malformed: 1, unknown: 1, deprecated: 1, total: 4 },
    })
    expect(summary.testCases).toEqual([
      { testCaseId: login.id, status: 'failed', resultCount: 4 },
      { testCaseId: logout.id, status: 'untested', resultCount: 0 },
    ])
    const parseErrors = await (await request.get(`${apiURL}/api/v1/test-runs/${runId}/parse-errors`)).json()
    expect(parseErrors.items).toMatchObject([
      { index: 6, persisted: true, severity: 'error' },
      { index: 7, persisted: false, severity: 'error' },
      { index: 8, persisted: true, severity: 'warning' },
    ])
    const slow = await (await request.get(`${apiURL}/api/v1/test-runs/${runId}/results?pageSize=100`)).json()
    expect(slow.items.find((r: { testName: string }) => r.testName.startsWith('login slow')).durationMs).toBeNull()
    const failed = await (await request.get(`${apiURL}/api/v1/test-runs/${runId}/results?status=failed`)).json()
    expect(failed.items).toHaveLength(1)
    const oldHistory = await (await request.get(`${apiURL}/api/v1/test-cases/${deprecated.id}/results`)).json()
    expect(oldHistory.items).toMatchObject([{ result: { correlation: 'deprecated', testCaseId: deprecated.id } }])
    const unknownTC = await request.get(`${apiURL}/api/v1/test-cases?pageSize=100`)
    expect((await unknownTC.json()).items.some((t: { id: number }) => t.id === 987654321)).toBe(false)
  })

  test('[BE-E2E-005] resending an attempt is idempotent and a rerun creates a new run', async ({ request, provenly }) => {
    const tc = await provenly.createTestCase({ title: 'API idempotency', automated: true })
    const runId = uniqueRunId()
    const report = junit(byProperty('idempotent', tc.id))
    const first = await (await provenly.ingest(runId, 1, report)).json()
    const replay = await provenly.ingest(runId, 1, report)
    expect(replay.status()).toBe(200)
    expect(await replay.json()).toMatchObject({ created: false, testRun: { id: first.testRun.id, resultCount: 1 } })
    const rerun = await provenly.ingest(runId, 2, report)
    expect(rerun.status()).toBe(201)
    const rerunBody = await rerun.json()
    expect(rerunBody.testRun.externalRunId).toBe(`github:${runId}:2`)
    expect(rerunBody.testRun.id).not.toBe(first.testRun.id)
    const old = await (await request.get(`${apiURL}/api/v1/test-runs/${first.testRun.id}`)).json()
    expect(old.resultCount).toBe(1)
    expect((await provenly.ingest(runId, 1, '<not xml')).status()).toBe(400)
  })

  test('[BE-E2E-006] history across runs and snapshot stability after catalog changes', async ({ request, provenly }) => {
    await provenly.isolateUniverse()
    const tc = await provenly.createTestCase({ title: 'API history', automated: true })
    const run1 = await (await provenly.ingest(uniqueRunId(), 1, junit(byProperty('history', tc.id)))).json()
    const run2 = await (await provenly.ingest(uniqueRunId(), 1, junit(byProperty('history renamed', tc.id, '<failure/>')))).json()
    const before = await (await request.get(`${apiURL}/api/v1/test-runs/${run1.testRun.id}/summary`)).json()

    await provenly.createTestCase({ title: 'API added later', automated: true })
    await request.patch(`${apiURL}/api/v1/test-cases/${tc.id}`, { data: { title: 'API history edited' } })
    await request.post(`${apiURL}/api/v1/test-cases/${tc.id}/deprecate`)

    const after = await (await request.get(`${apiURL}/api/v1/test-runs/${run1.testRun.id}/summary`)).json()
    expect(after).toEqual(before)
    const history = await (await request.get(`${apiURL}/api/v1/test-cases/${tc.id}/results`)).json()
    expect(history.items.map((h: { result: { status: string } }) => h.result.status)).toEqual(['failed', 'passed'])
    expect(history.items[0]).toMatchObject({ result: { testName: 'history renamed' }, run: { id: run2.testRun.id, commit: 'c0ffee1234' } })
    const runs = await (await request.get(`${apiURL}/api/v1/test-runs?pageSize=5`)).json()
    expect(runs.items[0].id).toBe(run2.testRun.id)
  })
})
