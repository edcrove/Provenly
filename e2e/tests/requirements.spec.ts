import { apiURL } from '../playwright.config'
import { byProperty, expect, junit, test, uniqueProjectKey, uniqueRunId } from '../support/fixtures'

const failure = '<failure message="boom">trace</failure>'

test.describe('Requirements and traceability', () => {
  test('[BE-E2E-018] requirements through the public API: native and imported ones, links and coverage from the latest results', async ({ request, provenly }) => {
    const key = uniqueProjectKey()
    await provenly.createProject(key, 'Requirements API')
    const login = await provenly.createTestCase({ title: 'login', project: key, automated: true })
    const pay = await provenly.createTestCase({ title: 'pay', project: key, automated: true })
    const base = `${apiURL}/api/v1/projects/${key}/requirements`

    const native = await request.post(base, { data: { title: 'Users sign in' } })
    expect(native.status()).toBe(201)
    const r1 = await native.json()
    expect(r1).toMatchObject({ provider: 'provenly', externalId: 'R-1', coverage: { status: 'uncovered', linked: 0 } })

    const imported = await request.post(`${base}/import`, {
      data: { provider: 'jira', items: [{ externalId: 'PAY-1', title: 'Pay by card', providerStatus: 'In progress' }] },
    })
    expect(imported.status()).toBe(200)
    expect(await imported.json()).toEqual({ created: 1, updated: 0 })
    const again = await request.post(`${base}/import`, {
      data: { provider: 'jira', items: [{ externalId: 'PAY-1', title: 'Pay by card or wallet' }] },
    })
    expect(await again.json()).toEqual({ created: 0, updated: 1 })
    const list = await (await request.get(base)).json()
    const jira = list.items.find((r: { externalId: string }) => r.externalId === 'PAY-1')
    expect(jira).toMatchObject({ provider: 'jira', title: 'Pay by card or wallet' })
    expect(jira.lastSyncedAt).toBeTruthy()

    expect((await request.put(`${base}/${r1.id}/test-cases`, { data: { testCaseIds: [login.id] } })).status()).toBe(200)
    expect((await request.put(`${base}/${jira.id}/test-cases`, { data: { testCaseIds: [login.id, pay.id] } })).status()).toBe(200)
    expect((await (await request.get(`${base}/${jira.id}`)).json()).coverage.status).toBe('not_run')

    const report = (...cases: string[]) =>
      request.post(`${apiURL}/api/v1/ingestion/junit?project=${key}&provider=github&runId=${uniqueRunId()}&runAttempt=1`, {
        headers: { 'Content-Type': 'application/xml' },
        data: junit(...cases),
      })
    expect((await report(byProperty('login', login.key), byProperty('pay', pay.key, failure))).status()).toBe(201)
    expect((await (await request.get(`${base}/${jira.id}`)).json()).coverage).toMatchObject({ status: 'failing', passed: 1, failed: 1 })
    expect((await (await request.get(`${base}/${r1.id}`)).json()).coverage.status).toBe('passing')

    expect((await report(byProperty('pay', pay.key))).status()).toBe(201)
    expect((await (await request.get(`${base}/${jira.id}`)).json()).coverage).toMatchObject({ status: 'passing', passed: 2, failed: 0 })

    const covered = await (await request.get(`${base}?testCase=${pay.id}`)).json()
    expect(covered.items.map((r: { externalId: string }) => r.externalId)).toEqual(['PAY-1'])

    expect((await request.post(base, { data: { provider: 'jira', externalId: 'PAY-1', title: 'dup' } })).status()).toBe(409)
    expect((await request.put(`${base}/${r1.id}/test-cases`, { data: { testCaseIds: [999999999] } })).status()).toBe(400)
    expect((await request.patch(`${base}/${r1.id}`, { data: { archived: true } })).status()).toBe(200)
    expect((await request.get(`${base}/999999999`)).status()).toBe(404)
  })

  test('[FE-E2E-020] a member writes a requirement, links a test case, CI reports it failing and the requirement and the test case show it', async ({ page, request, provenly }) => {
    const key = uniqueProjectKey()
    await provenly.createProject(key, 'Requirements UI')
    const login = await provenly.createTestCase({ title: 'Sign in works', project: key, automated: true })

    await page.goto('/requirements')
    await page.getByLabel('Current project').selectOption(key)
    await page.getByLabel('Title').fill('Users sign in with email')
    await page.getByRole('button', { name: 'Add requirement' }).click()
    await expect(page.getByTestId('requirement-R-1')).toContainText('Not covered')
    await page.getByRole('link', { name: 'R-1' }).click()
    await expect(page.getByText('No test case covers this requirement yet.')).toBeVisible()
    await page.getByLabel('Test case to link', { exact: true }).selectOption({ label: `${login.key} · Sign in works` })
    await page.getByRole('button', { name: 'Link test case' }).click()
    await expect(page.getByTestId(`covering-${login.id}`)).toContainText('not run')
    await expect(page.getByTestId('coverage-badge')).toHaveText('Not run')

    const res = await request.post(`${apiURL}/api/v1/ingestion/junit?project=${key}&provider=github&runId=${uniqueRunId()}&runAttempt=1`, {
      headers: { 'Content-Type': 'application/xml' },
      data: junit(byProperty('sign in', login.key, failure)),
    })
    expect(res.status()).toBe(201)
    await page.reload()
    await expect(page.getByTestId('coverage-badge')).toHaveText('Failing')
    await expect(page.getByTestId(`covering-${login.id}`)).toContainText('failed')

    await page.getByRole('link', { name: login.key }).click()
    await expect(page.getByTestId('covered-requirements')).toContainText('R-1')
    await expect(page.getByTestId('covered-requirements')).toContainText('Failing')
    await page.getByLabel('Current project').selectOption('')
  })
})
