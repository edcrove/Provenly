import { request as apiRequest } from '@playwright/test'

import { apiURL } from '../playwright.config'
import { expect, junit, byName, secret, test, uniqueProjectKey, uniqueRunId, uniqueUsername } from '../support/fixtures'

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
    // The project fixture may itself patch the project: any edit row of this project will do.
    const row = page.locator('[data-testid^="audit-"]').filter({ hasText: 'edited the project' }).first()
    await expect(row).toContainText(`/api/v1/projects/${key}`)
    await expect(page.locator('[data-testid^="audit-"]').filter({ hasNotText: key })).toHaveCount(0)
  })

  test('[FE-E2E-026] a test case edit reads in words, under its project, and the log filters by test case (card #48)', async ({ page, request, provenly }) => {
    const key = uniqueProjectKey()
    await provenly.createProject(key, 'Audit words')
    const tc = await provenly.createTestCase({ title: 'Refund', project: key })
    expect((await request.patch(`${apiURL}/api/v1/test-cases/${tc.id}`, { data: { title: 'Refund by card' } })).status()).toBe(200)
    await page.goto('/audit')
    await page.getByLabel('Test case').fill(tc.key.toLowerCase())
    await page.getByRole('button', { name: 'Filter' }).click()
    const rows = page.locator('[data-testid^="audit-"]')
    // Its creation names it too (deployed audit), newest first.
    await expect(rows).toHaveCount(2)
    await expect(rows.first()).toContainText(`edited ${tc.key}`)
    await expect(rows.first()).toContainText(`/api/v1/test-cases/${tc.id}`)
    await expect(rows.first()).toContainText(key)
    await expect(rows.nth(1)).toContainText(`created a test case ${tc.key}`)
  })

  test('[FE-E2E-038] a maintainer opens the audit log of their project and only theirs (P20-5)', async ({ browser, request, provenly }) => {
    const mine = uniqueProjectKey()
    const other = uniqueProjectKey()
    await provenly.createProject(mine, 'Maintained')
    await provenly.createProject(other, 'Not maintained')
    const { token } = await (await request.post(`${apiURL}/api/v1/invitations`, { data: { project: mine, role: 'maintainer' } })).json()
    const username = uniqueUsername()
    const anon = await apiRequest.newContext({ storageState: { cookies: [], origins: [] } })
    expect((await anon.post(`${apiURL}/api/v1/invitations/accept`, {
      data: { token, username, displayName: 'Maintainer', password: secret('maintainer password') },
    })).status()).toBe(201)
    await anon.dispose()
    const tc = await provenly.createTestCase({ title: 'Audited for its maintainer', project: mine })
    await provenly.createTestCase({ title: 'Not for them', project: other })

    const ctx = await browser.newContext({ storageState: { cookies: [], origins: [] } })
    const page = await ctx.newPage()
    await page.goto('/login')
    await page.getByLabel('Username').fill(username)
    await page.getByLabel('Password').fill(secret('maintainer password'))
    await page.getByRole('button', { name: 'Sign in' }).click()
    await page.getByRole('link', { name: 'Audit' }).first().click()
    await expect(page.getByText(`You see the changes to the projects you maintain: ${mine}.`)).toBeVisible()
    const rows = page.locator('[data-testid^="audit-"]')
    await expect(rows.filter({ hasText: `created a test case ${tc.key}` })).toHaveCount(1)
    await expect(rows.filter({ hasText: other })).toHaveCount(0)
    await expect(rows.filter({ hasNotText: mine })).toHaveCount(0)
    await ctx.close()
  })

  test('[BE-E2E-033] through the public API a maintainer reads only their projects\' events, without addresses (P20-5)', async ({ request, provenly }) => {
    const mine = uniqueProjectKey()
    await provenly.createProject(mine, 'Maintained API')
    const { token } = await (await request.post(`${apiURL}/api/v1/invitations`, { data: { project: mine, role: 'maintainer' } })).json()
    const username = uniqueUsername()
    const maintainer = await apiRequest.newContext({ storageState: { cookies: [], origins: [] } })
    expect((await maintainer.post(`${apiURL}/api/v1/invitations/accept`, {
      data: { token, username, displayName: 'Maintainer', password: secret('maintainer password') },
    })).status()).toBe(201)
    expect((await maintainer.post(`${apiURL}/api/v1/auth/login`, { data: { username, password: secret('maintainer password') } })).status()).toBe(200)
    await provenly.createTestCase({ title: 'Audited', project: mine })

    const page = await (await maintainer.get(`${apiURL}/api/v1/audit?pageSize=100`)).json()
    expect(page.totalItems).toBeGreaterThan(0)
    for (const e of page.items) expect(e).toMatchObject({ project: mine, ip: null, userAgent: null })
    expect((await maintainer.get(`${apiURL}/api/v1/audit?project=TC`)).status()).toBe(403)
    await maintainer.dispose()
  })
})
