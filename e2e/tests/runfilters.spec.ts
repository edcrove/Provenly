import { randomUUID } from 'node:crypto'

import { apiURL } from '../playwright.config'
import { expect, junit, test, uniqueRunId } from '../support/fixtures'

test.describe('Filtering runs (deployed audit)', () => {
  test('[BE-E2E-031] the runs list filters by branch, execution status and creation window through the API', async ({ request, provenly }) => {
    const branch = `e2e/${randomUUID().slice(0, 8)}`
    const ok = await provenly.ingest(uniqueRunId(), 1, junit(), { branch })
    const broken = await provenly.ingest(uniqueRunId(), 1, junit(), { branch, status: 'interrupted' })
    expect(ok.status()).toBe(201)
    expect(broken.status()).toBe(201)
    const id = async (res: Awaited<ReturnType<typeof provenly.ingest>>) => ((await res.json()) as { testRun: { id: number } }).testRun.id
    const list = async (q: Record<string, string>) => {
      const res = await request.get(`${apiURL}/api/v1/test-runs?${new URLSearchParams(q)}`)
      expect(res.status()).toBe(200)
      return ((await res.json()) as { items: { id: number }[] }).items.map((r) => r.id)
    }
    expect(await list({ branch })).toEqual([await id(broken), await id(ok)])
    expect(await list({ branch, executionStatus: 'interrupted' })).toEqual([await id(broken)])
    expect(await list({ branch, from: new Date(Date.now() + 3_600_000).toISOString() })).toEqual([])
    const bad = await request.get(`${apiURL}/api/v1/test-runs?from=2026-10-02T00:00:00Z&to=2026-10-01T00:00:00Z`)
    expect(bad.status()).toBe(400)
  })

  test('[FE-E2E-032] someone looking for their run filters the list by branch and execution in the UI', async ({ page, provenly }) => {
    const branch = `e2e/${randomUUID().slice(0, 8)}`
    const res = await provenly.ingest(uniqueRunId(), 1, junit(), { branch, status: 'interrupted' })
    const runId = ((await res.json()) as { testRun: { id: number } }).testRun.id
    await page.goto('/test-runs')
    await page.getByRole('textbox', { name: 'Branch' }).fill(branch)
    await page.keyboard.press('Enter')
    await expect(page).toHaveURL(new RegExp(`branch=${encodeURIComponent(branch)}`))
    await expect(page.getByRole('link', { name: `#${runId}`, exact: true })).toBeVisible()
    await expect(page.locator('tbody tr')).toHaveCount(1)
    await page.getByRole('combobox', { name: 'Execution' }).selectOption('completed')
    await expect(page.getByText('No runs match these filters.')).toBeVisible()
    await page.getByRole('button', { name: 'Clear filters' }).click()
    await expect(page).toHaveURL(/\/test-runs$/)
  })
})
