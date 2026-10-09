import { apiURL } from '../playwright.config'
import { byProperty, expect, junit, pickProject, test, uniqueProjectKey, uniqueRunId } from '../support/fixtures'

test.describe('Suites and partial runs (MVP D2)', () => {
  test('[BE-E2E-016] a run reported for a suite expects only the suite; the run names it and lists filter by it', async ({ request, provenly }) => {
    const key = uniqueProjectKey()
    await provenly.createProject(key, 'Suites API')
    const login = await provenly.createTestCase({ title: 'login', project: key, automated: true, tags: ['smoke'] })
    const pay = await provenly.createTestCase({ title: 'pay', project: key, automated: true })
    await provenly.createTestCase({ title: 'refund', project: key, automated: true })
    const suites = `${apiURL}/api/v1/projects/${key}/suites`
    expect((await request.post(suites, { data: { key: 'smoke', name: 'Smoke', kind: 'query', query: { tag: 'smoke' } } })).status()).toBe(201)
    expect((await request.post(suites, { data: { key: 'release', name: 'Release', kind: 'static', testCaseIds: [pay.id] } })).status()).toBe(201)

    const report = (suite: string) =>
      request.post(`${apiURL}/api/v1/ingestion/junit?project=${key}&suite=${suite}&provider=github&runId=${uniqueRunId()}&runAttempt=1`, {
        headers: { 'Content-Type': 'application/xml' },
        data: junit(byProperty('login', login.key), byProperty('pay', pay.key)),
      })
    const smoke = await report('smoke')
    expect(smoke.status()).toBe(201)
    const run = (await smoke.json()).testRun
    expect(run).toMatchObject({ expectedCount: 1, suite: { key: 'smoke', name: 'Smoke' }, outcome: { verdict: 'passed', untested: 0 } })
    const summary = await (await request.get(`${apiURL}/api/v1/test-runs/${run.id}/summary`)).json()
    expect(summary.outsideUniverse).toBe(1)

    expect((await report('release')).status()).toBe(201)
    const runs = await (await request.get(`${apiURL}/api/v1/test-runs?project=${key}&suite=smoke`)).json()
    expect(runs.items.map((r: { id: number }) => r.id)).toEqual([run.id])
    const members = await (await request.get(`${apiURL}/api/v1/test-cases?project=${key}&suite=release`)).json()
    expect(members.items.map((t: { key: string }) => t.key)).toEqual([pay.key])

    expect((await request.patch(`${suites}/smoke`, { data: { archived: true } })).status()).toBe(200)
    expect((await report('smoke')).status()).toBe(409)
    expect((await report('nope')).status()).toBe(404)
  })

  test('[FE-E2E-018] a maintainer builds a static suite in the UI; CI reports a run for it and the run shows the suite', async ({ page, request, provenly }) => {
    const key = uniqueProjectKey()
    await provenly.createProject(key, 'Suites UI')
    const login = await provenly.createTestCase({ title: 'Sign in works', project: key, automated: true })
    await provenly.createTestCase({ title: 'Checkout works', project: key, automated: true })

    await page.goto('/suites')
    await pickProject(page, key)
    await page.getByLabel('Name', { exact: true }).fill('Release candidate')
    await page.getByLabel('Key', { exact: true }).fill('release')
    await page.getByLabel('Kind').selectOption('static')
    await page.getByRole('button', { name: 'Create suite' }).click()
    await page.getByRole('link', { name: 'Release candidate' }).click()
    await expect(page.getByText('This suite lists no test cases yet.')).toBeVisible()
    await page.getByLabel('Test case to add', { exact: true }).selectOption({ label: `${login.key} · Sign in works` })
    await page.getByRole('button', { name: 'Add to suite' }).click()
    await expect(page.getByTestId(`suite-case-${login.key}`)).toBeVisible()
    await expect(page.getByTestId('suite-ci-hint')).toContainText(`&project=${key}&suite=release`)

    const res = await request.post(`${apiURL}/api/v1/ingestion/junit?project=${key}&suite=release&provider=github&runId=${uniqueRunId()}&runAttempt=1`, {
      headers: { 'Content-Type': 'application/xml' },
      data: junit(byProperty('sign in', login.key)),
    })
    expect(res.status()).toBe(201)
    await page.getByRole('link', { name: 'Runs of this suite' }).click()
    await expect(page.getByTestId('suite-filter')).toContainText(`Runs of suite ${key}/release`)
    expect(new URL(page.url()).searchParams.get('project')).toBe(key)
    await page.getByRole('link', { name: `#${(await res.json()).testRun.id}` }).click()
    await expect(page.getByTestId('suite-badge')).toHaveText('suite: Release candidate')
    await expect(page.getByTestId('verdict-badge').first()).toContainText('passed')
    await pickProject(page, '')
  })
})
