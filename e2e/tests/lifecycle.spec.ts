import { apiURL } from '../playwright.config'
import { expect, test, uniqueProjectKey, uniqueRunId } from '../support/fixtures'

test.describe('Run lifecycle (CHG-003, Ed 2026-11-06)', () => {
  test('[BE-E2E-034] CI finalizes a live run whose report will not arrive; a manual run is refused and a batch run is left as it is', async ({ request, provenly }) => {
    const key = uniqueProjectKey()
    await provenly.createProject(key, 'Lifecycle API')
    const runId = uniqueRunId()
    const finalize = (id: string, provider = 'github') =>
      request.post(`${apiURL}/api/v1/ingestion/finalize?project=${key}&provider=${provider}&runId=${id}&runAttempt=1`)

    const live = await (await request.post(`${apiURL}/api/v1/test-runs/live`, { data: { project: key, provider: 'github', runId, runAttempt: 1 } })).json()
    expect(live).toMatchObject({ mode: 'live', executionStatus: 'running', closedReason: null })
    const done = await finalize(runId)
    expect(done.status()).toBe(201)
    expect((await done.json()).testRun).toMatchObject({ id: live.id, executionStatus: 'interrupted', closedReason: 'finalized' })
    expect((await finalize(runId)).status()).toBe(200)

    const manual = await (await request.post(`${apiURL}/api/v1/test-runs/manual`, { data: { project: key, name: 'Sign-off' } })).json()
    expect((await finalize(manual.providerRunId, manual.provider)).status()).toBe(409)
    expect((await request.post(`${apiURL}/api/v1/test-runs/${manual.id}/finish`, { data: { status: 'cancelled' } })).status()).toBe(200)
  })

  test('[FE-E2E-041] archiving a project names its running runs, ends them and their pages say why', async ({ page, request, provenly }) => {
    const key = uniqueProjectKey()
    await provenly.createProject(key, 'Archive with runs')
    const manual = await (await request.post(`${apiURL}/api/v1/test-runs/manual`, { data: { project: key, name: 'Sign-off' } })).json()

    await page.goto(`/projects?q=${key}`)
    const row = page.getByTestId(`project-${key}`)
    await row.getByRole('button', { name: 'Archive…' }).click()
    const confirm = row.getByRole('group', { name: `Archive ${key}` })
    await expect(confirm.getByTestId('archive-running-runs')).toHaveText(`It also ends its running run: #${manual.id} (manual: cancelled).`)
    await confirm.getByRole('button', { name: 'Archive', exact: true }).click()
    await expect(page.getByText(`No project matches “${key}”.`)).toBeVisible()

    await page.goto(`/test-runs/${manual.id}`)
    const alert = page.getByTestId('interrupted-run')
    await expect(alert).toContainText('This run ended when its project was archived')
    await expect(alert).toContainText('It was still being executed by hand: it was cancelled.')
    expect(await (await request.get(`${apiURL}/api/v1/test-runs/${manual.id}`)).json()).toMatchObject({ executionStatus: 'cancelled', closedReason: 'archived' })
  })
})
