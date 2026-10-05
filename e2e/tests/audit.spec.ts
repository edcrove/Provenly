import { apiURL } from '../playwright.config'
import { expect, junit, byName, test, uniqueProjectKey, uniqueRunId } from '../support/fixtures'

test.describe('Audit log (prototype feature 20)', () => {
  test('[BE-E2E-026] changes by users and by CI API keys are audited and only administrators read them', async ({ playwright, request, provenly }) => {
    const key = uniqueProjectKey()
    await provenly.createProject(key, 'Audit API')
    expect((await request.patch(`${apiURL}/api/v1/projects/${key}`, { data: { name: 'Audit API renamed' } })).status()).toBe(200)
    const apiKey = await (await request.post(`${apiURL}/api/v1/projects/${key}/api-keys`, { data: { name: 'CI' } })).json()
    const ci = await playwright.request.newContext({ storageState: { cookies: [], origins: [] } })
    const ingested = await ci.post(`${apiURL}/api/v1/ingestion/junit?project=${key}&provider=github&runId=${uniqueRunId()}&runAttempt=1`, {
      headers: { Authorization: `Bearer ${apiKey.token}`, 'Content-Type': 'application/xml' },
      data: junit(byName('anything')),
    })
    expect(ingested.status()).toBe(201)

    const events = (await (await request.get(`${apiURL}/api/v1/audit?project=${key}`)).json()).items
    expect(events.map((e: { action: string }) => e.action)).toEqual([
      'POST /api/v1/ingestion/junit',
      'POST /api/v1/projects/{projectKey}/api-keys',
      'PATCH /api/v1/projects/{projectKey}',
    ])
    expect(events[0].actor).toBe(`api key ${apiKey.apiKey.prefix}… (CI)`)
    expect(events[2]).toMatchObject({ actor: 'admin', path: `/api/v1/projects/${key}`, status: 200, project: key })
    expect(JSON.stringify(events)).not.toContain(apiKey.token)
    expect((await request.get(`${apiURL}/api/v1/audit?project=bad`)).status()).toBe(400)
    expect((await ci.get(`${apiURL}/api/v1/audit`, { headers: { Authorization: `Bearer ${apiKey.token}` } })).status()).toBe(401)
    await ci.dispose()
  })

  test('[FE-E2E-026] an administrator opens the audit log and filters it by project', async ({ page, request, provenly }) => {
    const key = uniqueProjectKey()
    await provenly.createProject(key, 'Audit UI')
    expect((await request.patch(`${apiURL}/api/v1/projects/${key}`, { data: { description: 'audited' } })).status()).toBe(200)
    await page.goto('/test-cases')
    await page.getByRole('link', { name: 'Audit' }).click()
    await page.getByLabel('Project', { exact: true }).fill(key)
    await page.getByRole('button', { name: 'Filter' }).click()
    // The project fixture may itself patch the project: any PATCH row of this project will do.
    const row = page.locator('[data-testid^="audit-"]').filter({ hasText: 'PATCH /api/v1/projects/{projectKey}' }).first()
    await expect(row).toContainText(`/api/v1/projects/${key}`)
    await expect(page.locator('[data-testid^="audit-"]').filter({ hasNotText: key })).toHaveCount(0)
  })
})
