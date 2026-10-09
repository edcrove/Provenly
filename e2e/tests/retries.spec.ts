import { apiURL } from '../playwright.config'
import { expect, junit, test, uniqueProjectKey, uniqueRunId } from '../support/fixtures'

test.describe('Retries and flaky tests (D1)', () => {
  test('[BE-E2E-013] Surefire retries through the API: every attempt is stored, the last one counts, a pass after failing is flaky', async ({ request, provenly }) => {
    const key = uniqueProjectKey()
    await provenly.createProject(key, 'Retries')
    const flaky = await provenly.createTestCase({ title: 'checkout loads', automated: true, project: key })
    const broken = await provenly.createTestCase({ title: 'pay with voucher', automated: true, project: key })
    const report = junit(
      `<testcase name="checkout ${flaky.key}" time="3"><flakyFailure message="timeout"><stackTrace>at page.goto</stackTrace></flakyFailure></testcase>`,
      `<testcase name="voucher ${broken.key}"><failure message="first"/><rerunFailure message="again"/></testcase>`,
    )
    const res = await provenly.ingest(uniqueRunId(), 1, report, { project: key })
    expect(res.status()).toBe(201)
    const body = await res.json()
    expect(body).toMatchObject({ received: 4, persisted: 4 })
    const runURL = `${apiURL}/api/v1/test-runs/${body.testRun.id}`

    const run = await (await request.get(runURL)).json()
    expect(run.outcome).toMatchObject({ verdict: 'failed', passed: 1, failed: 1, flaky: 1 })
    const summary = await (await request.get(`${runURL}/summary`)).json()
    expect(summary.testCases).toEqual(expect.arrayContaining([
      expect.objectContaining({ testCaseKey: flaky.key, status: 'passed', flaky: true, resultCount: 2 }),
      expect.objectContaining({ testCaseKey: broken.key, status: 'failed', flaky: false }),
    ]))
    const results = await (await request.get(`${runURL}/results`)).json()
    expect(results.items.map((r: { testName: string; attempt: number; retried: boolean; status: string }) =>
      `${r.testName.split(' ')[0]}#${r.attempt} ${r.status}${r.retried ? ' retried' : ''}`)).toEqual([
      // The test whose last attempt failed comes first; attempts stay together (deployed audit).
      'voucher#1 failed retried', 'voucher#2 failed', 'checkout#1 failed retried', 'checkout#2 passed',
    ])
    expect(results.items[2].errorDetails).toBe('at page.goto')
  })

  test('[FE-E2E-016] a flaky run in the UI: flaky badge, flaky test case and retried attempts', async ({ page, provenly }) => {
    const key = uniqueProjectKey()
    await provenly.createProject(key, 'UI retries')
    const tc = await provenly.createTestCase({ title: 'Search suggests products', automated: true, project: key })
    const xml = junit(`<testcase name="search ${tc.key}"><properties><property name="retry" value="0"/></properties><failure message="flaky network"/></testcase>`,
      `<testcase name="search ${tc.key}"><properties><property name="retry" value="1"/></properties></testcase>`)
    const runId = (await (await provenly.ingest(uniqueRunId(), 1, xml, { project: key })).json()).testRun.id

    await page.goto(`/test-runs/${runId}`)
    await expect(page.getByTestId('flaky-badge')).toHaveText('1 flaky')
    await expect(page.getByTestId('verdict-badge')).toContainText(/passed/i)
    await expect(page.getByTestId('flaky-cases')).toContainText(tc.key)
    await expect(page.getByTestId('attempt-badge')).toHaveText(['attempt 1 · retried', 'attempt 2'])
    await page.goto(`/test-cases/${tc.id}`)
    await expect(page.getByTestId('attempt-badge').first()).toBeVisible()
  })
})
