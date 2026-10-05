import { apiURL } from '../playwright.config'
import { byProperty, expect, junit, test, uniqueProjectKey, uniqueRunId } from '../support/fixtures'

test.describe('Snapshot amendment (DEC-42)', () => {
  test('[BE-E2E-012] a maintainer includes a reported manual test case in a run; the run is marked as edited', async ({ request, provenly }) => {
    const key = uniqueProjectKey()
    await provenly.createProject(key, 'Amendments')
    const auto = await provenly.createTestCase({ title: 'auto', automated: true, project: key })
    const manual = await provenly.createTestCase({ title: 'manual by mistake', project: key })
    const ingest = await provenly.ingest(uniqueRunId(), 1, junit(byProperty('a', auto.key), byProperty('m', manual.key, '<failure message="x"/>')), { project: key })
    const runId = (await ingest.json()).testRun.id
    const url = `${apiURL}/api/v1/test-runs/${runId}`

    let summary = await (await request.get(`${url}/summary`)).json()
    expect(summary.outsideUniverseTestCaseIds).toEqual([manual.id])
    expect((await (await request.get(url)).json()).outcome.verdict).toBe('passed')

    const amended = await request.post(`${url}/amendments`, { data: { testCaseId: manual.id, reason: 'marked manual by mistake' } })
    expect(amended.status()).toBe(201)
    expect((await request.post(`${url}/amendments`, { data: { testCaseId: manual.id, reason: 'again' } })).status()).toBe(409)
    expect((await request.post(`${url}/amendments`, { data: { testCaseId: 987654, reason: 'x' } })).status()).toBe(400)

    summary = await (await request.get(`${url}/summary`)).json()
    expect(summary).toMatchObject({ snapshotTotal: 1, expectedTotal: 2, amendedTestCaseIds: [manual.id], outsideUniverseTestCaseIds: [] })
    const run = await (await request.get(url)).json()
    expect(run).toMatchObject({ amendmentCount: 1, expectedCount: 2 })
    expect(run.outcome.verdict).toBe('failed')
    const history = await (await request.get(`${url}/amendments`)).json()
    expect(history.items).toMatchObject([{ testCaseKey: manual.key, reason: 'marked manual by mistake', amendedByUsername: 'admin' }])
  })

  test('[FE-E2E-015] a maintainer includes a test case from the run page and sees the edited mark and its history', async ({ page, provenly }) => {
    const key = uniqueProjectKey()
    await provenly.createProject(key, 'UI amendments')
    const manual = await provenly.createTestCase({ title: 'Export to PDF', project: key })
    const runId = (await (await provenly.ingest(uniqueRunId(), 1, junit(byProperty('pdf', manual.key)), { project: key })).json()).testRun.id

    await page.goto(`/test-runs/${runId}`)
    const item = page.getByTestId(`outside-${manual.id}`)
    await item.getByLabel('Why it belongs in this run').fill('Automated since sprint 12; flag was stale')
    await item.getByRole('button', { name: 'Include in this run' }).click()
    await expect(page.getByTestId('edited-badge')).toBeVisible()
    await expect(page.getByTestId('amendments')).toContainText('Automated since sprint 12')
    await expect(page.getByTestId('verdict-badge')).toContainText(/passed/i)

    await page.goto('/test-runs')
    await expect(page.getByRole('row', { name: new RegExp(`#${runId}\\b`) }).getByTestId('edited-badge')).toBeVisible()
  })
})
