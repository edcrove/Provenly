import { apiURL } from '../playwright.config'
import { expect, test, uniqueProjectKey, uniqueRunId } from '../support/fixtures'
import ProvenlyReporter, { type PwTestCase, type PwTestResult } from '../../reporters/playwright/src/index'

const pwTest = (id: string, title: string, tags: string[] = []): PwTestCase => ({
  id,
  title,
  titlePath: () => ['', 'checkout.spec.ts', title],
  location: { file: 'checkout.spec.ts' },
  annotations: [],
  tags,
})

const pwResult = (retry: number, status: PwTestResult['status'], message = ''): PwTestResult => ({
  retry,
  status,
  duration: 1200,
  startTime: new Date(),
  errors: message ? [{ message, stack: `Error: ${message}` }] : [],
})

test.describe('Playwright reporter (DEC-15)', () => {
  test('[BE-E2E-022] the reporter streams a live run with a project API key and its final report completes it, flaky retries included', async ({ request, provenly }) => {
    const key = uniqueProjectKey()
    await provenly.createProject(key, 'Reporter')
    const pay = await provenly.createTestCase({ title: 'pay', project: key, automated: true })
    const refund = await provenly.createTestCase({ title: 'refund', project: key, automated: true })
    const token = (await (await request.post(`${apiURL}/api/v1/projects/${key}/api-keys`, { data: { name: 'Playwright' } })).json()).token as string
    const logs: string[] = []
    const reporter = new ProvenlyReporter({
      url: apiURL, apiKey: token, provider: 'github', runId: uniqueRunId(), runAttempt: 1, pipeline: 'e2e', branch: 'main', env: {},
      log: (m) => logs.push(m),
    })

    reporter.onBegin()
    const payTest = pwTest('t1', `pays by card ${pay.key}`)
    const refundTest = pwTest('t2', 'refunds an order', [`@${refund.key}`])
    reporter.onTestBegin(payTest, pwResult(0, 'passed'))
    reporter.onTestEnd(payTest, pwResult(0, 'passed'))
    reporter.onTestBegin(refundTest, pwResult(0, 'failed'))
    reporter.onTestEnd(refundTest, pwResult(0, 'failed', 'refund not issued'))
    reporter.onTestBegin(refundTest, pwResult(1, 'passed'))
    reporter.onTestEnd(refundTest, pwResult(1, 'passed'))
    await reporter.onEnd({ status: 'passed' })
    expect(logs).toEqual([])

    const runs = await (await request.get(`${apiURL}/api/v1/test-runs?project=${key}`)).json()
    expect(runs.items).toHaveLength(1)
    const run = runs.items[0]
    expect(run).toMatchObject({ mode: 'live', executionStatus: 'completed', pipeline: 'e2e', outcome: { verdict: 'passed', flaky: 1, passed: 2 } })
    const live = await (await request.get(`${apiURL}/api/v1/test-runs/${run.id}/live`)).json()
    expect(live).toMatchObject({ reconciliation: 'consistent', runFinished: true, events: 7, finished: 2 })
    const results = await (await request.get(`${apiURL}/api/v1/test-runs/${run.id}/results`)).json()
    expect(results.items.map((r: { attempt: number; status: string }) => `${r.attempt}:${r.status}`).sort()).toEqual(['1:failed', '1:passed', '2:passed'])
  })
})
