import { apiURL } from '../playwright.config'
import { expect, test, uniqueProjectKey } from '../support/fixtures'

test.describe('Manual execution (MVP D3)', () => {
  test('[BE-E2E-017] a manual run through the API: started, results recorded and re-tested, completed and closed', async ({ request, provenly }) => {
    const key = uniqueProjectKey()
    await provenly.createProject(key, 'Sign-off API')
    const checkout = await provenly.createTestCase({ title: 'checkout by hand', project: key })
    await provenly.createTestCase({ title: 'automated', project: key, automated: true })
    const start = await request.post(`${apiURL}/api/v1/test-runs/manual`, { data: { project: key, name: 'Sign-off' } })
    expect(start.status()).toBe(201)
    const run = await start.json()
    expect(run).toMatchObject({ mode: 'manual', executionStatus: 'running', expectedCount: 1, startedBy: 'admin' })

    const record = (status: string, extra = {}) =>
      request.post(`${apiURL}/api/v1/test-runs/${run.id}/manual-results`, { data: { testCaseId: checkout.id, status, ...extra } })
    expect((await record('failed', { note: 'Pay button missing', failedStep: 2 })).status()).toBe(201)
    const retest = await record('passed')
    expect(await retest.json()).toMatchObject({ attempt: 2, recordedBy: 'admin' })
    const finished = await request.post(`${apiURL}/api/v1/test-runs/${run.id}/finish`, { data: { status: 'completed' } })
    expect(await finished.json()).toMatchObject({ executionStatus: 'completed', outcome: { verdict: 'passed', flaky: 0 } })
    expect((await record('failed')).status()).toBe(409)
    const history = await (await request.get(`${apiURL}/api/v1/test-cases/${checkout.id}/results`)).json()
    expect(history.items.map((r: { result: { status: string } }) => r.result.status)).toEqual(['passed', 'failed'])
  })

  test('[FE-E2E-019] a tester runs a manual run in the UI: records a failure, re-tests it and completes the run', async ({ page, provenly }) => {
    const key = uniqueProjectKey()
    await provenly.createProject(key, 'Sign-off UI')
    const tc = await provenly.createTestCase({ title: 'Refund by hand', project: key })

    await page.goto('/test-runs')
    await page.getByRole('link', { name: 'Start manual run' }).click()
    await page.getByLabel('What is being tested').fill('Release 2.4 sign-off')
    await page.getByLabel('Project', { exact: true }).selectOption(key)
    await page.getByRole('button', { name: 'Start manual run' }).click()
    const row = page.getByTestId(`manual-${tc.key}`)
    await row.getByLabel(`Note for ${tc.key}`).fill('Refund amount wrong')
    await row.getByLabel(`Failed step of ${tc.key}`).fill('3')
    await row.getByRole('button', { name: 'Fail' }).click()
    await expect(row.getByTestId('status-badge')).toHaveText('failed')
    await row.getByRole('button', { name: 'Pass' }).click()
    await expect(row.getByTestId('status-badge')).toHaveText('passed')
    await page.getByRole('button', { name: 'Complete run' }).click()
    await expect(page.getByTestId('manual-execution')).toBeHidden()
    await expect(page.getByTestId('verdict-badge').first()).toHaveText('passed')
    await expect(page.getByText('Refund amount wrong')).toBeVisible()

    // Results of a manual run are what a manual test case expects: no "receives automated results" warning.
    await page.goto(`/test-cases/${tc.id}`)
    await expect(page.getByRole('link', { name: /^manual:/ }).first()).toBeVisible()
    await expect(page.getByTestId('manual-with-results')).toBeHidden()
  })
})
