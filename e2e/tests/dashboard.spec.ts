import { apiURL } from '../playwright.config'
import { byProperty, expect, junit, pickProject, test, uniqueProjectKey, uniqueRunId } from '../support/fixtures'

const retried = (name: string, key: string) =>
  `<testcase name="${name}" classname="suite"><properties><property name="tc-id" value="${key}"/><property name="attempt" value="1"/></properties><failure message="x"/></testcase>` +
  `<testcase name="${name}" classname="suite"><properties><property name="tc-id" value="${key}"/><property name="attempt" value="2"/></properties></testcase>`

test.describe('Quality dashboard', () => {
  test('[BE-E2E-020] project quality through the public API: automation, never-executed and flaky test cases', async ({ request, provenly }) => {
    const key = uniqueProjectKey()
    await provenly.createProject(key, 'Quality API')
    const login = await provenly.createTestCase({ title: 'login', project: key, automated: true })
    const pay = await provenly.createTestCase({ title: 'pay', project: key, automated: true })
    await provenly.createTestCase({ title: 'receipt', project: key })
    const report = (...cases: string[]) =>
      request.post(`${apiURL}/api/v1/ingestion/junit?project=${key}&provider=github&runId=${uniqueRunId()}&runAttempt=1`, {
        headers: { 'Content-Type': 'application/xml' },
        data: junit(...cases),
      })
    expect((await report(retried('login', login.key))).status()).toBe(201)
    expect((await report(retried('login', login.key), byProperty('pay', pay.key))).status()).toBe(201)

    const quality = `${apiURL}/api/v1/projects/${key}/quality`
    const q = await (await request.get(quality)).json()
    expect(q.testCases).toEqual({ active: 3, automated: 2, manual: 1, automationRate: 66.67 })
    expect(q.execution).toMatchObject({ staleDays: 14, neverExecuted: 1, stale: 0 })
    expect(q.execution.testCases.map((c: { testCaseKey: string }) => c.testCaseKey)).toHaveLength(1)
    expect(q.flaky).toEqual({ window: 20, testCases: [{ testCaseId: login.id, testCaseKey: login.key, runs: 2 }] })
    expect((await (await request.get(`${quality}?window=1`)).json()).flaky.testCases[0].runs).toBe(1)
    for (const qs of ['staleDays=0', 'staleDays=366', 'window=201', 'window=', 'window=x']) {
      expect((await request.get(`${quality}?${qs}`)).status(), qs).toBe(400)
    }
    expect((await request.get(`${apiURL}/api/v1/projects/NOPE99/quality`)).status()).toBe(404)
  })

  test('[FE-E2E-022] the dashboard shows a project\'s latest run, trend, automation, flaky test cases and requirement coverage', async ({ page, request, provenly }) => {
    const key = uniqueProjectKey()
    await provenly.createProject(key, 'Quality UI')
    const login = await provenly.createTestCase({ title: 'Sign in works', project: key, automated: true })
    await provenly.createTestCase({ title: 'Receipt looks right', project: key })
    const report = async (...cases: string[]) => {
      const res = await request.post(`${apiURL}/api/v1/ingestion/junit?project=${key}&provider=github&runId=${uniqueRunId()}&runAttempt=1`, {
        headers: { 'Content-Type': 'application/xml' },
        data: junit(...cases),
      })
      expect(res.status()).toBe(201)
      return (await res.json()).testRun.id as number
    }
    const first = await report(byProperty('sign in', login.key, '<failure message="x"/>'))
    const second = await report(retried('sign in', login.key))
    const req = await request.post(`${apiURL}/api/v1/projects/${key}/requirements`, { data: { title: 'Users sign in' } })
    await request.put(`${apiURL}/api/v1/projects/${key}/requirements/${(await req.json()).id}/test-cases`, { data: { testCaseIds: [login.id] } })

    await page.goto('/dashboard')
    await pickProject(page, key)
    await expect(page.getByTestId('latest-run')).toContainText(`#${second}`)
    await expect(page.getByTestId('latest-run-rates')).toContainText(/^Executed \d+ of \d+/)
    await expect(page.getByTestId(`trend-${first}`)).toBeVisible()
    await expect(page.getByTestId('automation-rate')).toHaveText('50%')
    await expect(page.getByTestId('stale-cases')).toContainText('never executed')
    await expect(page.getByTestId('flaky-cases')).toContainText(`${login.key} flaky in 1 run`)
    await expect(page.getByTestId('coverage-breakdown')).toContainText('Passing: 1')
    await page.getByTestId(`trend-${first}`).click()
    await expect(page.getByTestId('verdict-badge').first()).toContainText('failed')
    await pickProject(page, '')
  })
})
