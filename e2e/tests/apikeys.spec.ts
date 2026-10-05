import { request as apiRequest } from '@playwright/test'

import { apiURL } from '../playwright.config'
import { byName, expect, junit, test, uniqueProjectKey, uniqueRunId } from '../support/fixtures'

const empty = { cookies: [], origins: [] }
const xml = { 'Content-Type': 'application/xml' }

test.describe('CI API keys', () => {
  test('[BE-E2E-010] CI reports with a project API key: its project by default, nothing else, until revoked', async ({ request, provenly }) => {
    const key = uniqueProjectKey()
    await provenly.createProject(key, 'Keys project')
    const tc = await provenly.createTestCase({ title: 'reported by CI', automated: true, project: key })
    const created = await request.post(`${apiURL}/api/v1/projects/${key}/api-keys`, { data: { name: 'GitHub Actions' } })
    expect(created.status()).toBe(201)
    const { token, apiKey } = await created.json()
    expect(token).toMatch(/^pvk_[0-9a-f]{8}_/)

    const anon = await apiRequest.newContext({ storageState: empty })
    const ci = await apiRequest.newContext({ storageState: empty, extraHTTPHeaders: { Authorization: `Bearer ${token}` } })
    const ingest = (ctx: typeof ci, qs = '') =>
      ctx.post(`${apiURL}/api/v1/ingestion/junit?provider=github&runId=${uniqueRunId()}&runAttempt=1${qs}`, {
        headers: xml,
        data: junit(byName(`ci ${tc.key}`)),
      })

    expect((await ingest(anon)).status()).toBe(401)
    const res = await ingest(ci)
    expect(res.status()).toBe(201)
    const body = await res.json()
    expect(body.persisted).toBe(1)
    expect(body.diagnostics).toEqual([])
    expect((await ingest(ci, '&project=TC')).status()).toBe(404)
    expect((await ci.get(`${apiURL}/api/v1/test-runs`)).status()).toBe(401)

    const list = await (await request.get(`${apiURL}/api/v1/projects/${key}/api-keys`)).json()
    expect(list.items[0]).toMatchObject({ id: apiKey.id, status: 'active' })
    expect(list.items[0].lastUsedAt).not.toBeNull()
    expect(JSON.stringify(list)).not.toContain(token)

    expect((await request.post(`${apiURL}/api/v1/projects/${key}/api-keys/${apiKey.id}/revoke`)).status()).toBe(200)
    expect((await ingest(ci)).status()).toBe(401)
    await ci.dispose()
    await anon.dispose()
  })

  test('[FE-E2E-013] a maintainer creates a key in the UI, CI reports with it, and the run shows up', async ({ page, provenly }) => {
    const key = uniqueProjectKey()
    await provenly.createProject(key, 'UI keys')
    const tc = await provenly.createTestCase({ title: 'checked by CI', automated: true, project: key })

    // Straight to the project's page: the paginated project list may show it on another page.
    await page.goto(`/projects/${key}`)
    await page.getByLabel('Key name').fill('Nightly pipeline')
    await page.getByRole('button', { name: 'Create API key' }).click()
    const token = (await page.getByTestId('api-key-token').textContent())!
    await expect(page.getByTestId('api-key-snippet')).toContainText(`project=${key}`)
    await expect(page.getByTestId('api-key-snippet')).toContainText('Content-Encoding: gzip')

    const runId = uniqueRunId()
    const ci = await apiRequest.newContext({ storageState: empty, extraHTTPHeaders: { Authorization: `Bearer ${token}` } })
    const res = await ci.post(`${apiURL}/api/v1/ingestion/junit?provider=github&runId=${runId}&runAttempt=1`, {
      headers: xml,
      data: junit(byName(`nightly ${tc.key}`)),
    })
    expect(res.status()).toBe(201)
    await ci.dispose()

    await page.reload()
    const row = page.getByRole('row', { name: /Nightly pipeline/ })
    await expect(row).not.toContainText('Never')
    await row.getByRole('button', { name: 'Revoke' }).click()
    await expect(row).toContainText('revoked')

    await page.goto(`/test-cases/${tc.id}`)
    await expect(page.getByText(`github:${runId}:1`)).toBeVisible()
  })
})
